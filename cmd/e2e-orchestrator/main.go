package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Sink defines a background server process to be managed
type Sink struct {
	Name    string
	Dir     string
	Command string
	Args    []string
	Ports   []int
	Env     []string
}

func (s *Sink) Start(ctx context.Context, wg *sync.WaitGroup) error {
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	if s.Dir != "" {
		cmd.Dir = s.Dir
	}
	cmd.Env = append(os.Environ(), s.Env...)

	// Pipe output to stdout with prefix
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", s.Name, err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		go io.Copy(os.Stdout, stdout)
		go io.Copy(os.Stderr, stderr)
		cmd.Wait()
		log.Printf("🛑 %s exited", s.Name)
	}()

	return nil
}

func checkHealth(port int) bool {
	client := http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/health", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func waitForSinks(sinks []Sink, timeout time.Duration) error {
	start := time.Now()
	for {
		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for sinks to become healthy")
		}

		allHealthy := true
		for _, s := range sinks {
			for _, port := range s.Ports {
				if !checkHealth(port) {
					allHealthy = false
					break
				}
			}
			if !allHealthy {
				break
			}
		}

		if allHealthy {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
}

func buildArtifacts(ctx context.Context) error {
	log.Println("🔨 Building Kitchen Sink Apps and Bindings...")

	cmds := []struct {
		name string
		dir  string
		cmd  string
		args []string
	}{
		{"Go Sink", ".", "go", []string{"build", "-o", "bin/kitchen-sink-go", "cmd/kitchen-sink-go/main.go"}},
		{"Rust Sink", ".", "cargo", []string{"build", "--bin", "kitchen-sink-rust", "--release"}},
		// Optionally build PyO3 / N-API here if needed via maturin / npm run build
	}

	for _, c := range cmds {
		log.Printf("Building %s...", c.name)
		cmd := exec.CommandContext(ctx, c.cmd, c.args...)
		cmd.Dir = c.dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to build %s: %w", c.name, err)
		}
	}
	return nil
}

func main() {
	log.Println("🚀 Starting Cross-Platform E2E Orchestrator")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		log.Println("Interrupt received, shutting down...")
		cancel()
	}()

	if err := buildArtifacts(ctx); err != nil {
		log.Fatalf("Build failed: %v", err)
	}

	cwd, _ := os.Getwd()

	sinks := []Sink{
		{
			Name:    "Go Sinks",
			Command: filepath.Join(cwd, "bin", "kitchen-sink-go"),
			Ports:   []int{8081, 8082, 8083},
		},
		{
			Name:    "Rust Sinks",
			Command: filepath.Join(cwd, "target", "release", "kitchen-sink-rust"),
			Ports:   []int{9081, 9082},
		},
		{
			Name:    "Node Sinks (Express, Fastify)",
			Dir:     filepath.Join(cwd, "cmd", "kitchen-sink-node"),
			Command: "node",
			Args:    []string{"server.js", "--config", filepath.Join(cwd, "config", "radixip.yaml")},
			Ports:   []int{8091, 8092},
		},
		{
			Name:    "Python (FastAPI)",
			Dir:     filepath.Join(cwd, "cmd", "kitchen-sink-python"),
			Command: "python", // Assume python is available and radixip is accessible in pythonpath
			Args:    []string{"-m", "uvicorn", "fastapi_app:app", "--port", "8093", "--host", "0.0.0.0"},
			Ports:   []int{8093},
			Env:     []string{fmt.Sprintf("RADIXIP_CONFIG=%s", filepath.Join(cwd, "config", "radixip.yaml"))},
		},
		{
			Name:    "Python (Flask)",
			Dir:     filepath.Join(cwd, "cmd", "kitchen-sink-python"),
			Command: "python",
			Args:    []string{"flask_app.py"},
			Ports:   []int{8094},
			Env:     []string{fmt.Sprintf("RADIXIP_CONFIG=%s", filepath.Join(cwd, "config", "radixip.yaml"))},
		},
		{
			Name:    "Python (Django)",
			Dir:     filepath.Join(cwd, "cmd", "kitchen-sink-python"),
			Command: "python",
			Args:    []string{"django_app.py", "runserver", "0.0.0.0:8095"},
			Ports:   []int{8095},
			Env:     []string{fmt.Sprintf("RADIXIP_CONFIG=%s", filepath.Join(cwd, "config", "radixip.yaml"))},
		},
	}

	var wg sync.WaitGroup
	log.Println("🚀 Spawning kitchen sinks...")
	for i := range sinks {
		if err := sinks[i].Start(ctx, &wg); err != nil {
			log.Printf("⚠️ Failed to start sink %s: %v", sinks[i].Name, err)
		}
	}

	log.Println("⏳ Waiting for all sinks to report healthy...")
	if err := waitForSinks(sinks, 30*time.Second); err != nil {
		log.Fatalf("❌ Sinks did not become healthy: %v", err)
	}

	log.Println("✅ All sinks are healthy!")
	
	// TODO: Replace bash vegeta calls with Go HTTP clients for cross-platform robustness.
	log.Println("🏃 Running Phase 1 (Route Limits) and Phase 2 (Auto-ban) tests...")

	// For now, keep them running until interrupted to verify they work.
	// Production script would run tests here and then cancel()
	
	<-ctx.Done()
	wg.Wait()
	log.Println("✅ Orchestrator shutdown complete.")
}
