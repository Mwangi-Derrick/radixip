// E2E Orchestrator: Cross-platform test runner for RadixIP kitchen sinks.
//
// Replaces scripts/sequential_test.sh with a fully portable Go binary that:
//   1. Downloads vegeta and ghz (self-contained, no system deps)
//   2. Builds all kitchen sink binaries (Go, Rust, Node, Python)
//   3. Spawns all framework servers as managed subprocesses
//   4. Runs Phase 1 (route-trie rate limits), Phase 2 (auto-ban), Phase 3 (gRPC)
//   5. Saves all results to files
//
// Usage:
//   go run ./cmd/e2e-orchestrator [flags]
//   go run ./cmd/e2e-orchestrator --skip-build --results-dir ./results
//   go run ./cmd/e2e-orchestrator --require-all   # CI: fail if any sink unhealthy

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// Flags
// ---------------------------------------------------------------------------

var (
	flagSkipBuild  = flag.Bool("skip-build", false, "Skip building binaries (assume they already exist)")
	flagResultsDir = flag.String("results-dir", ".", "Directory to write result files into")
	flagConfig     = flag.String("config", "config/radixip.yaml", "Path to radixip.yaml config file")
	flagSinkOnly   = flag.Bool("sink-only", false, "Only start sinks, do not run tests (useful for manual testing)")
	flagRequireAll = flag.Bool("require-all", false, "Fail if any expected sink is unhealthy (recommended for CI)")
)

// ---------------------------------------------------------------------------
// Tool installation (vegeta + ghz)
// ---------------------------------------------------------------------------

func vegetaInstallURL() (string, string) {
	const version = "12.11.1"
	var goos, goarch string
	switch runtime.GOOS {
	case "linux":
		goos = "linux"
	case "darwin":
		goos = "darwin"
	case "windows":
		goos = "windows"
	default:
		goos = runtime.GOOS
	}
	switch runtime.GOARCH {
	case "amd64":
		goarch = "amd64"
	case "arm64":
		goarch = "arm64"
	default:
		goarch = runtime.GOARCH
	}
	filename := fmt.Sprintf("vegeta_%s_%s_%s.tar.gz", version, goos, goarch)
	url := fmt.Sprintf("https://github.com/tsenart/vegeta/releases/download/v%s/%s", version, filename)
	return url, filename
}

func downloadAndExtract(ctx context.Context, url, destDir, binName string) error {
	log.Printf("📥 Downloading %s from %s...", binName, url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	tmpFile := filepath.Join(destDir, binName+".tar.gz")
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	if _, err = io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	f.Close()

	cmd := exec.CommandContext(ctx, "tar", "-xzf", tmpFile, "-C", destDir, binName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract failed: %w\n%s", err, out)
	}
	os.Remove(tmpFile)
	log.Printf("✅ %s installed to %s", binName, destDir)
	return nil
}

func installVegeta(ctx context.Context, binDir string) error {
	url, _ := vegetaInstallURL()
	binName := "vegeta"
	if runtime.GOOS == "windows" {
		binName = "vegeta.exe"
	}
	return downloadAndExtract(ctx, url, binDir, binName)
}

func installGhz(ctx context.Context, binDir string) error {
	log.Println("📥 Installing ghz via `go install`...")
	cmd := exec.CommandContext(ctx, "go", "install", "github.com/bojand/ghz/cmd/ghz@latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install ghz failed: %w", err)
	}
	gopath, err := exec.CommandContext(ctx, "go", "env", "GOPATH").Output()
	if err != nil {
		return fmt.Errorf("go env GOPATH failed: %w", err)
	}
	ghzBin := "ghz"
	if runtime.GOOS == "windows" {
		ghzBin = "ghz.exe"
	}
	src := filepath.Join(strings.TrimSpace(string(gopath)), "bin", ghzBin)
	dst := filepath.Join(binDir, ghzBin)
	if src != dst {
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("ghz not found at %s: %w", src, err)
		}
		if err := os.WriteFile(dst, data, 0755); err != nil {
			return err
		}
	}
	log.Printf("✅ ghz installed to %s", binDir)
	return nil
}

func ensureTools(ctx context.Context, binDir string) (vegetaBin, ghzBin string, err error) {
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", "", err
	}
	vegetaBin = filepath.Join(binDir, "vegeta")
	ghzBin = filepath.Join(binDir, "ghz")
	if runtime.GOOS == "windows" {
		vegetaBin += ".exe"
		ghzBin += ".exe"
	}

	if _, err := os.Stat(vegetaBin); os.IsNotExist(err) {
		if err := installVegeta(ctx, binDir); err != nil {
			return "", "", fmt.Errorf("vegeta install: %w", err)
		}
	} else {
		log.Printf("✅ vegeta already cached at %s", vegetaBin)
	}

	if _, err := os.Stat(ghzBin); os.IsNotExist(err) {
		if err := installGhz(ctx, binDir); err != nil {
			return "", "", fmt.Errorf("ghz install: %w", err)
		}
	} else {
		log.Printf("✅ ghz already cached at %s", ghzBin)
	}

	return vegetaBin, ghzBin, nil
}

// ---------------------------------------------------------------------------
// Building
// ---------------------------------------------------------------------------

// pythonWheelInstallScript returns a small Python program (invoked with
// `python -c`) that finds the newest wheel in wheelDir and pip-installs it.
// Doing the glob inside Python keeps this portable across shells and OSes.
func pythonWheelInstallScript(wheelDir string) string {
	return fmt.Sprintf(`
import glob, os, subprocess, sys
wheels = sorted(glob.glob(os.path.join(%q, "*.whl")), key=os.path.getmtime, reverse=True)
if not wheels:
    sys.exit("no wheel found in " + %q)
sys.exit(subprocess.call([sys.executable, "-m", "pip", "install", "--force-reinstall", wheels[0]]))
`, wheelDir, wheelDir)
}

func buildArtifacts(ctx context.Context, cwd string) error {
	log.Println("🔨 Building Kitchen Sink Apps...")
	steps := []struct {
		name string
		dir  string
		cmd  string
		args []string
	}{
		{"Go Sink", cwd, "go", []string{"build", "-o", filepath.Join(cwd, "bin", "kitchen-sink-go"), "./cmd/kitchen-sink-go/..."}},
		{"Go gRPC Probe", cwd, "go", []string{"build", "-o", filepath.Join(cwd, "bin", "grpc-probe-go"), "./cmd/grpc-probe-go/..."}},
		{"Rust Sink", cwd, "cargo", []string{"build", "--bin", "kitchen-sink-rust", "--release"}},
		{"Node Binding Install", filepath.Join(cwd, "lib", "node"), "npm", []string{"install"}},
		{"Node Binding Build", filepath.Join(cwd, "lib", "node"), "npm", []string{"run", "build"}},
		{"Node Sink Install", filepath.Join(cwd, "cmd", "kitchen-sink-node"), "npm", []string{"install"}},
		{"Python Maturin", cwd, "python", []string{"-m", "pip", "install", "maturin"}},
		// NOTE: `maturin build` (NOT `develop`) — develop requires an active
		// virtualenv, which CI runners do not have. We build a wheel and then
		// pip-install it explicitly in the next step.
		{"Python Binding Build", filepath.Join(cwd, "lib", "python"), "python", []string{"-m", "maturin", "build", "--release"}},
		{"Python Binding Install", cwd, "python", []string{"-c", pythonWheelInstallScript(filepath.Join(cwd, "target", "wheels"))}},
		// We don't use --no-deps because we need django, fastapi, flask to be installed.
		// radixip is commented out in pyproject.toml, so it won't clobber the wheel.
		{"Python Sink Install", filepath.Join(cwd, "cmd", "kitchen-sink-python"), "python", []string{"-m", "pip", "install", "-e", "."}},
	}
	for _, s := range steps {
		log.Printf("  Building %s...", s.name)
		cmdName := s.cmd
		if runtime.GOOS == "windows" && cmdName == "npm" {
			cmdName = "npm.cmd"
		}
		cmd := exec.CommandContext(ctx, cmdName, s.args...)
		cmd.Dir = s.dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build %s: %w", s.name, err)
		}
		log.Printf("  ✅ %s done", s.name)
	}
	log.Println("✅ All builds complete")
	return nil
}

// ---------------------------------------------------------------------------
// Sink process management
// ---------------------------------------------------------------------------

type Sink struct {
	Name  string
	Cmd   string
	Args  []string
	Dir   string
	Env   []string
	Ports []int
	proc  *exec.Cmd
}

func (s *Sink) Start(ctx context.Context, wg *sync.WaitGroup) error {
	s.proc = exec.CommandContext(ctx, s.Cmd, s.Args...)
	s.proc.Dir = s.Dir
	s.proc.Env = append(os.Environ(), s.Env...)
	s.proc.Stdout = os.Stdout
	s.proc.Stderr = os.Stderr
	setProcGroup(s.proc) // platform-specific; see procgroup_*.go
	if err := s.proc.Start(); err != nil {
		return fmt.Errorf("start %s: %w", s.Name, err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.proc.Wait()
		log.Printf("🛑 %s exited", s.Name)
	}()
	return nil
}

func healthCheck(port int) bool {
	c := &http.Client{Timeout: 1 * time.Second}
	resp, err := c.Get(fmt.Sprintf("http://localhost:%d/health", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func waitHealthy(sinks []Sink, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		all := true
		for _, s := range sinks {
			for _, p := range s.Ports {
				if !healthCheck(p) {
					all = false
				}
			}
		}
		if all {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	var bad []string
	for _, s := range sinks {
		for _, p := range s.Ports {
			if !healthCheck(p) {
				bad = append(bad, fmt.Sprintf("%s:%d", s.Name, p))
			}
		}
	}
	return fmt.Errorf("timed out waiting for sinks: %s", strings.Join(bad, ", "))
}

// ---------------------------------------------------------------------------
// Load testing (vegeta + ghz)
// ---------------------------------------------------------------------------

type VegetaReport struct {
	Success     float64        `json:"success"`
	StatusCodes map[string]int `json:"status_codes"`
	Latencies   struct {
		P50  int64 `json:"50th"`
		P95  int64 `json:"95th"`
		P99  int64 `json:"99th"`
		Max  int64 `json:"max"`
		Mean int64 `json:"mean"`
	} `json:"latencies"`
	Throughput float64 `json:"throughput"`
	Requests   int64   `json:"requests"`
}

func runVegeta(ctx context.Context, vegetaBin, target, label, resultsDir string, rate int, duration time.Duration) (*VegetaReport, error) {
	targetsFile := filepath.Join(resultsDir, label+"_target.txt")
	if err := os.WriteFile(targetsFile, []byte(target), 0644); err != nil {
		return nil, err
	}

	attackArgs := []string{
		"attack",
		"-rate=" + strconv.Itoa(rate),
		"-duration=" + duration.String(),
		"-targets=" + targetsFile,
	}

	attack := exec.CommandContext(ctx, vegetaBin, attackArgs...)
	report := exec.CommandContext(ctx, vegetaBin, "report", "-type=json")

	pipe, err := attack.StdoutPipe()
	if err != nil {
		return nil, err
	}
	report.Stdin = pipe

	var reportOut bytes.Buffer
	report.Stdout = &reportOut
	report.Stderr = os.Stderr

	if err := report.Start(); err != nil {
		return nil, err
	}
	if err := attack.Start(); err != nil {
		return nil, err
	}
	if err := attack.Wait(); err != nil {
		return nil, fmt.Errorf("vegeta attack: %w", err)
	}
	pipe.Close()
	if err := report.Wait(); err != nil {
		return nil, fmt.Errorf("vegeta report: %w", err)
	}

	outFile := filepath.Join(resultsDir, label+"_result.json")
	os.WriteFile(outFile, reportOut.Bytes(), 0644)

	var result VegetaReport
	if err := json.Unmarshal(reportOut.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("parse vegeta json: %w", err)
	}
	return &result, nil
}

// runHTTPBatch sends an exact, small number of requests concurrently.  Unlike
// a rate-based attack this is deterministic for development WSGI servers,
// which can otherwise process only part of a one-second Vegeta window.  It is
// intentionally used only for Phase 1's policy-semantic checks; Phase 2 keeps
// the high-rate Vegeta attack that validates auto-ban under load.
func runHTTPBatch(ctx context.Context, method, url, ip, label, resultsDir string, count int) (*VegetaReport, error) {
	if count <= 0 {
		return nil, fmt.Errorf("request count must be positive")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	report := &VegetaReport{StatusCodes: make(map[string]int), Requests: int64(count)}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, method, url, nil)
			if err == nil {
				req.Header.Set("X-Forwarded-For", ip)
				resp, doErr := client.Do(req)
				if doErr == nil {
					err = nil
					status := resp.StatusCode
					resp.Body.Close()
					mu.Lock()
					report.StatusCodes[strconv.Itoa(status)]++
					mu.Unlock()
					return
				}
				err = doErr
			}
			mu.Lock()
			report.StatusCodes["0"]++
			mu.Unlock()
			_ = err // transport failures are represented as Vegeta's status code 0.
		}()
	}
	wg.Wait()

	var successes int
	for status, n := range report.StatusCodes {
		code, err := strconv.Atoi(status)
		if err == nil && code >= 200 && code < 300 {
			successes += n
		}
	}
	report.Success = float64(successes) / float64(count)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(resultsDir, label+"_result.json"), data, 0644); err != nil {
		return nil, err
	}
	return report, nil
}

type GhzReport struct {
	Count          int            `json:"count"`
	Total          float64        `json:"total"`
	Average        float64        `json:"average"`
	Fastest        float64        `json:"fastest"`
	Slowest        float64        `json:"slowest"`
	Rps            float64        `json:"rps"`
	ErrorDistrib   map[string]int `json:"errorDistribution"`
	StatusCodeDist map[string]int `json:"statusCodeDistribution"`
}

func runGhz(ctx context.Context, ghzBin, protoDir, port, ip, label, resultsDir string) (*GhzReport, error) {
	outFile := filepath.Join(resultsDir, label+"_ghz.json")
	args := []string{
		"--insecure",
		"--proto", filepath.Join(protoDir, "proto", "radixip", "v1", "radixip.proto"),
		"--call", "radixip.v1.RadixService/Lookup",
		"-n", "2000",
		"-c", "64",
		"-m", fmt.Sprintf(`{"x-forwarded-for":"%s"}`, ip),
		"--format", "json",
		"--output", outFile,
		"localhost:" + port,
	}
	cmd := exec.CommandContext(ctx, ghzBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("⚠️  ghz exited non-zero for %s (expected during auto-ban tests)", label)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		return nil, fmt.Errorf("read ghz output: %w", err)
	}
	var report GhzReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse ghz json: %w", err)
	}
	return &report, nil
}

// ---------------------------------------------------------------------------
// Test phases
// ---------------------------------------------------------------------------

type TestSummary struct {
	Phase   string
	Name    string
	Port    int
	Passed  bool
	Details string
}

// artifactLabel converts a display name into one filename component.  Sink
// names are user-facing and may contain separators (for example "Net/HTTP"),
// which must never be interpreted as a subdirectory under resultsDir.
func artifactLabel(name string) string {
	label := strings.ToLower(name)
	label = strings.NewReplacer(
		" ", "_",
		"(", "",
		")", "",
		"/", "_",
		"\\", "_",
	).Replace(label)
	return strings.Trim(label, "_")
}

func phase1RouteTrie(ctx context.Context, resultsDir string, summaries *[]TestSummary) {
	log.Println("\n==========================================")
	log.Println(" Phase 1: Route-Trie Specific Rate Limits")
	log.Println("==========================================")

	targets := []struct {
		name     string
		port     int
		ipSuffix int
	}{
		{"Gin (Go)", 8081, 1},
		{"Echo (Go)", 8082, 2},
		{"Fiber (Go)", 8083, 3},
		{"Chi (Go)", 8084, 4},
		{"Net/HTTP (Go)", 8085, 5},
		{"Axum (Rust)", 9081, 6},
		{"Actix (Rust)", 9082, 7},
		{"Express (Node)", 8091, 8},
		{"Fastify (Node)", 8092, 9},
		{"FastAPI (Python)", 8093, 10},
		{"Flask (Python)", 8096, 11},
		{"Django (Python)", 8095, 12},
	}

	for _, t := range targets {
		if !healthCheck(t.port) {
			log.Printf("⏭️  Skipping %s (port %d not healthy)", t.name, t.port)
			*summaries = append(*summaries, TestSummary{
				Phase: "1", Name: t.name + " (SKIPPED)", Port: t.port,
				Passed: false, Details: "port not healthy",
			})
			continue
		}
		log.Printf("\nTesting %s on port %d...", t.name, t.port)
		label := artifactLabel(t.name)

		authIP := fmt.Sprintf("203.0.113.%d", t.ipSuffix)
		labelAuth := fmt.Sprintf("p1_%s_auth", label)
		authURL := fmt.Sprintf("http://localhost:%d/api/v1/auth", t.port)
		// Keep this below the five-violation auto-ban threshold.  Phase 1 is
		// validating the route limiter (429), while Phase 2 exclusively owns
		// the auto-ban (403) lifecycle.  Flooding here contaminated later
		// framework checks when sinks intentionally share a policy engine.
		authReport, err := runHTTPBatch(ctx, http.MethodPost, authURL, authIP, labelAuth, resultsDir, 9)
		if err != nil {
			log.Printf("⚠️  request batch error for %s auth: %v", t.name, err)
			*summaries = append(*summaries, TestSummary{Phase: "1", Name: t.name + " auth", Port: t.port, Passed: false, Details: err.Error()})
		} else {
			status429 := authReport.StatusCodes["429"]
			passed := status429 > 0 && authReport.StatusCodes["200"] > 0 && authReport.StatusCodes["403"] == 0
			details := fmt.Sprintf("success=%.3f 429=%d 200=%d", authReport.Success, status429, authReport.StatusCodes["200"])
			if authReport.StatusCodes["200"] == 0 && status429 == 0 {
				details += fmt.Sprintf(" (codes: %v)", authReport.StatusCodes)
			}
			log.Printf("  Auth (capacity=5): %s", details)
			*summaries = append(*summaries, TestSummary{Phase: "1", Name: t.name + " auth", Port: t.port, Passed: passed, Details: details})
		}

		pubIP := fmt.Sprintf("203.0.114.%d", t.ipSuffix)
		labelPub := fmt.Sprintf("p1_%s_public", label)
		publicURL := fmt.Sprintf("http://localhost:%d/api/v1/public", t.port)
		// This request count exceeds the auth route's capacity but is well below
		// the public route's capacity, proving that the route trie selected the
		// public policy without triggering the unrelated auto-ban test.
		pubReport, err := runHTTPBatch(ctx, http.MethodGet, publicURL, pubIP, labelPub, resultsDir, 9)
		if err != nil {
			log.Printf("⚠️  request batch error for %s public: %v", t.name, err)
			*summaries = append(*summaries, TestSummary{Phase: "1", Name: t.name + " public", Port: t.port, Passed: false, Details: err.Error()})
		} else {
			details := fmt.Sprintf("success=%.3f 429=%d 200=%d", pubReport.Success, pubReport.StatusCodes["429"], pubReport.StatusCodes["200"])
			if pubReport.StatusCodes["200"] == 0 && pubReport.StatusCodes["429"] == 0 {
				details += fmt.Sprintf(" (codes: %v)", pubReport.StatusCodes)
			}
			if pubReport.Requests == 0 {
				details += " ⚠️ zero requests"
			}
			log.Printf("  Public (capacity=1000): %s", details)
			passed := pubReport.Success > 0.5 && pubReport.StatusCodes["403"] == 0
			*summaries = append(*summaries, TestSummary{Phase: "1", Name: t.name + " public", Port: t.port, Passed: passed, Details: details})
		}
	}
}

func phase2AutoBan(ctx context.Context, vegetaBin, resultsDir string, summaries *[]TestSummary) {
	log.Println("\n==========================================")
	log.Println(" Phase 2: Auto-Ban Trigger & Sweeper Test")
	log.Println("==========================================")

	banIP := "203.0.113.200"

	for _, t := range []struct {
		name string
		port int
	}{
		{"Gin (Go)", 8081},
		{"Axum (Rust)", 9081},
		{"Express (Node)", 8091},
		{"FastAPI (Python)", 8093},
	} {
		log.Printf("\nAuto-ban test: %s on port %d...", t.name, t.port)

		if !healthCheck(t.port) {
			log.Printf("⏭️  Skipping %s (port %d not healthy)", t.name, t.port)
			*summaries = append(*summaries, TestSummary{
				Phase: "2", Name: t.name + " (SKIPPED)", Port: t.port,
				Passed: false, Details: "port not healthy",
			})
			continue
		}

		label := fmt.Sprintf("p2_%s", strings.ToLower(strings.ReplaceAll(t.name, " ", "_")))
		target := fmt.Sprintf("GET http://localhost:%d/api/v1/public\nX-Forwarded-For: %s\n", t.port, banIP)
		report, err := runVegeta(ctx, vegetaBin, target, label, resultsDir, 5000, 5*time.Second)
		if err != nil {
			log.Printf("⚠️  vegeta error: %v", err)
			*summaries = append(*summaries, TestSummary{Phase: "2", Name: t.name, Port: t.port, Passed: false, Details: err.Error()})
			continue
		}
		status403 := report.StatusCodes["403"]
		status429 := report.StatusCodes["429"]
		passed := status403 > 0
		details := fmt.Sprintf("429=%d 403(auto-ban)=%d", status429, status403)
		if passed {
			log.Printf("  ✅ Auto-Ban triggered! %s", details)
		} else {
			log.Printf("  ❌ Auto-Ban did NOT trigger. %s", details)
		}
		*summaries = append(*summaries, TestSummary{Phase: "2", Name: t.name, Port: t.port, Passed: passed, Details: details})
	}

	log.Println("\n⏳ Waiting 35s for bans to expire...")
	time.Sleep(35 * time.Second)

	c := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:8081/api/v1/public"), nil)
	req.Header.Set("X-Forwarded-For", banIP)
	resp, err := c.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			log.Println("✅ Sweeper successfully lifted the ban")
			*summaries = append(*summaries, TestSummary{Phase: "2", Name: "Sweeper (Gin)", Port: 8081, Passed: true, Details: "ban lifted after 35s"})
		} else {
			log.Printf("❌ IP still blocked after 35s (status=%d)", resp.StatusCode)
			*summaries = append(*summaries, TestSummary{Phase: "2", Name: "Sweeper (Gin)", Port: 8081, Passed: false, Details: fmt.Sprintf("status=%d", resp.StatusCode)})
		}
	}
}

func phase3GRPC(ctx context.Context, ghzBin, grpcProbe, protoDir, resultsDir string, summaries *[]TestSummary) {
	log.Println("\n==========================================")
	log.Println(" Phase 3: gRPC Auto-Ban & Sweeper Test  ")
	log.Println("==========================================")

	for _, t := range []struct {
		name string
		port string
		ip   string
	}{
		{"Go gRPC", "50051", "203.0.113.251"},
		{"Rust gRPC", "50052", "203.0.113.252"},
	} {
		log.Printf("\nExercising %s on port %s...", t.name, t.port)
		label := fmt.Sprintf("p3_%s", strings.ToLower(strings.ReplaceAll(t.name, " ", "_")))
		report, err := runGhz(ctx, ghzBin, protoDir, t.port, t.ip, label, resultsDir)
		if err != nil {
			log.Printf("⚠️  ghz error: %v", err)
		} else {
			log.Printf("  ghz: rps=%.1f total=%d", report.Rps, report.Count)
		}

		probeOut, err := exec.CommandContext(ctx, grpcProbe,
			"-target", "localhost:"+t.port,
			"-requests", "2000",
			"-concurrency", "64",
			"-ip", t.ip,
		).CombinedOutput()

		probeFile := filepath.Join(resultsDir, label+"_probe.txt")
		os.WriteFile(probeFile, probeOut, 0644)

		passed := strings.Contains(string(probeOut), "PermissionDenied")
		details := "no PermissionDenied"
		if passed {
			details = "PermissionDenied (auto-ban confirmed)"
		}
		if err != nil && !passed {
			details = fmt.Sprintf("probe error: %v", err)
		}
		if passed {
			log.Printf("  ✅ %s gRPC auto-ban triggered", t.name)
		} else {
			log.Printf("  ❌ %s gRPC auto-ban did NOT trigger", t.name)
		}
		*summaries = append(*summaries, TestSummary{Phase: "3", Name: t.name, Port: 0, Passed: passed, Details: details})
	}

	log.Println("⏳ Waiting 35s for gRPC bans to expire...")
	time.Sleep(35 * time.Second)
	for _, t := range []struct {
		name string
		port string
		ip   string
	}{
		{"Go gRPC", "50051", "203.0.113.251"},
		{"Rust gRPC", "50052", "203.0.113.252"},
	} {
		out, _ := exec.CommandContext(ctx, grpcProbe,
			"-target", "localhost:"+t.port,
			"-requests", "1",
			"-concurrency", "1",
			"-ip", t.ip,
		).CombinedOutput()
		lifted := strings.Contains(string(out), "OK, 1")
		if lifted {
			log.Printf("✅ %s gRPC sweeper lifted the ban", t.name)
		} else {
			log.Printf("❌ %s gRPC ban still active", t.name)
		}
		*summaries = append(*summaries, TestSummary{Phase: "3", Name: t.name + " sweeper", Passed: lifted})
	}
}

// ---------------------------------------------------------------------------
// Save summary
// ---------------------------------------------------------------------------

func saveSummary(summaries []TestSummary, resultsDir string) {
	outFile := filepath.Join(resultsDir, "test_summary.txt")
	var sb strings.Builder
	sb.WriteString("╔══════════════════════════════════════════════════════════════╗\n")
	sb.WriteString("║          RadixIP E2E Test Summary                            ║\n")
	sb.WriteString("╚══════════════════════════════════════════════════════════════╝\n\n")

	pass, fail := 0, 0
	for _, s := range summaries {
		icon := "✅"
		if !s.Passed {
			icon = "❌"
			fail++
		} else {
			pass++
		}
		sb.WriteString(fmt.Sprintf("[Phase %s] %s %s — %s\n", s.Phase, icon, s.Name, s.Details))
	}
	sb.WriteString(fmt.Sprintf("\nTotal: %d passed, %d failed\n", pass, fail))

	content := sb.String()
	fmt.Println("\n" + content)
	os.WriteFile(outFile, []byte(content), 0644)
	log.Printf("📄 Summary written to %s", outFile)
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-quit; log.Println("Interrupt, shutting down..."); cancel() }()

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}

	resultsDir := *flagResultsDir
	os.MkdirAll(resultsDir, 0755)

	binDir := filepath.Join(cwd, "bin")
	os.MkdirAll(binDir, 0755)

	// 1. Ensure test tools
	log.Println("🔧 Ensuring test tools (vegeta, ghz)...")
	vegetaBin, ghzBin, err := ensureTools(ctx, binDir)
	if err != nil {
		log.Fatalf("❌ Tool setup failed: %v", err)
	}

	// 2. Build
	if !*flagSkipBuild {
		if err := buildArtifacts(ctx, cwd); err != nil {
			log.Fatalf("❌ Build failed: %v", err)
		}
	} else {
		log.Println("⏭️  Skipping build (--skip-build)")
	}

	configAbs := *flagConfig
	if !filepath.IsAbs(configAbs) {
		configAbs = filepath.Join(cwd, configAbs)
	}

	// 3. Spawn sinks — all ecosystems by default.
	coreSinks := []Sink{
		{
			Name:  "Go Sinks (Gin/Echo/Fiber/gRPC)",
			Cmd:   filepath.Join(binDir, "kitchen-sink-go"),
			Ports: []int{8081, 8082, 8083},
		},
		{
			Name:  "Rust Sinks (Axum/Actix/gRPC)",
			Cmd:   filepath.Join(cwd, "target", "release", "kitchen-sink-rust"),
			Ports: []int{9081, 9082},
		},
	}

	if runtime.GOOS == "windows" {
		coreSinks[0].Cmd += ".exe"
		coreSinks[1].Cmd += ".exe"
	}

	var allSinks []Sink
	allSinks = append(allSinks, coreSinks...)

	allSinks = append(allSinks,
		Sink{
			Name:  "Node Sinks (Express/Fastify)",
			Dir:   filepath.Join(cwd, "cmd", "kitchen-sink-node"),
			Cmd:   "node",
			Args:  []string{"server.js", "--config", configAbs},
			Ports: []int{8091, 8092},
		},
		Sink{
			Name:  "Python FastAPI",
			Dir:   filepath.Join(cwd, "cmd", "kitchen-sink-python"),
			Cmd:   "python",
			Args:  []string{"-m", "uvicorn", "fastapi_app:app", "--port", "8093", "--host", "0.0.0.0"},
			Ports: []int{8093},
			Env:   []string{"RADIXIP_CONFIG=" + configAbs},
		},
		Sink{
			Name:  "Python Flask",
			Dir:   filepath.Join(cwd, "cmd", "kitchen-sink-python"),
			Cmd:   "python",
			Args:  []string{"-m", "flask", "--app", "flask_app", "run", "--host", "0.0.0.0", "--port", "8096", "--no-reload"},
			Ports: []int{8096},
			Env:   []string{"RADIXIP_CONFIG=" + configAbs},
		},
		Sink{
			Name: "Python Django",
			Dir:  filepath.Join(cwd, "cmd", "kitchen-sink-python"),
			Cmd:  "python",
			// NOTE: assumes django_app.py has an `if __name__ == "__main__"` block
			// that calls execute_from_command_line. If this is a standard Django
			// project, replace with: manage.py runserver 0.0.0.0:8095 --noreload
			Args:  []string{"django_app.py", "runserver", "0.0.0.0:8095", "--noreload"},
			Ports: []int{8095},
			Env:   []string{"RADIXIP_CONFIG=" + configAbs},
		},
	)

	// On Unix, prefer `python3` if `python` isn't on PATH.
	for i, s := range allSinks {
		if s.Cmd == "python" && runtime.GOOS != "windows" {
			if _, err := exec.LookPath("python"); err != nil {
				if _, err := exec.LookPath("python3"); err == nil {
					allSinks[i].Cmd = "python3"
				}
			}
		}
	}

	var wg sync.WaitGroup
	log.Println("🚀 Spawning sinks...")
	for i := range allSinks {
		if err := allSinks[i].Start(ctx, &wg); err != nil {
			log.Printf("⚠️  Failed to start %s: %v", allSinks[i].Name, err)
		}
	}

	log.Println("⏳ Waiting for sinks to become healthy...")
	if err := waitHealthy(allSinks, 45*time.Second); err != nil {
		if *flagRequireAll {
			log.Fatalf("❌ --require-all set and health check failed: %v", err)
		}
		log.Printf("⚠️  Health check: %v", err)
	} else {
		log.Println("✅ All sinks healthy!")
	}

	if *flagSinkOnly {
		log.Println("--sink-only: sinks running. Ctrl-C to stop.")
		<-ctx.Done()
		wg.Wait()
		return
	}

	// 4. Run test phases
	var summaries []TestSummary

	grpcProbeBin := filepath.Join(binDir, "grpc-probe-go")
	if runtime.GOOS == "windows" {
		grpcProbeBin += ".exe"
	}
	if _, err := os.Stat(grpcProbeBin); err != nil {
		log.Fatalf("❌ grpc-probe-go not found at %s (did you skip the build?)", grpcProbeBin)
	}

	phase1RouteTrie(ctx, resultsDir, &summaries)
	phase2AutoBan(ctx, vegetaBin, resultsDir, &summaries)
	phase3GRPC(ctx, ghzBin, grpcProbeBin, cwd, resultsDir, &summaries)

	// 5. Save results
	saveSummary(summaries, resultsDir)

	failures := 0
	for _, s := range summaries {
		if !s.Passed {
			failures++
		}
	}

	cancel()
	wg.Wait()
	log.Println("✅ Orchestrator done.")

	if failures > 0 {
		os.Exit(1)
	}
}
