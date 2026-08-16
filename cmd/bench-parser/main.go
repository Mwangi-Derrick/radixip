// Command bench-parser reads raw Go (`go test -bench -benchmem -v`) and
// Rust (`cargo bench -- --output-format bencher`) benchmark logs, classifies
// every result by tree family / operation / IP version / concurrency
// variant, aggregates repeated samples, and renders a small static site
// (one page per runtime and per tree family, plus an overview and a
// Go-vs-Rust comparison page) into an output directory suitable for
// publishing straight to GitHub Pages.
//
// Usage:
//
//	bench-parser \
//	  -go-log go_bench_results.txt \
//	  -rust-log rust_bench_results.txt \
//	  -out docs/benchmarks
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	goLog := flag.String("go-log", "", "path to raw go test -bench log")
	rustLog := flag.String("rust-log", "", "path to raw cargo bench (bencher format) log")
	outDir := flag.String("out", "docs/benchmarks", "output directory for the generated site")
	flag.Parse()

	var all []classifiedSample

	if *goLog != "" {
		f, err := os.Open(*goLog)
		if err != nil {
			fatalf("opening go log: %v", err)
		}
		all = append(all, parseGoLog(f)...)
		f.Close()
	}
	if *rustLog != "" {
		f, err := os.Open(*rustLog)
		if err != nil {
			fatalf("opening rust log: %v", err)
		}
		all = append(all, parseRustLog(f)...)
		f.Close()
	}

	if len(all) == 0 {
		fatalf("no benchmark samples parsed from the provided logs — check paths and formats")
	}

	stats := aggregate(all)
	analysis := buildAnalysis(stats)

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatalf("creating output dir: %v", err)
	}

	// ---- pages -------------------------------------------------------
	write(*outDir, "index.html", pageShell("Overview", "overview", "", renderOverview(analysis, "")))

	for _, runtime := range []string{"go", "rust"} {
		write(filepath.Join(*outDir, runtime), "index.html",
			pageShell(runtimeLabel(runtime), runtime, "../", renderRuntimeIndex(analysis, runtime, "../")))

		for _, fam := range families {
			write(filepath.Join(*outDir, runtime, fam), "index.html",
				pageShell(runtimeLabel(runtime)+" · "+familyLabel(fam), runtime, "../../",
					renderFamilyPage(analysis, runtime, fam, "../../")))
		}
	}

	write(filepath.Join(*outDir, "compare"), "index.html",
		pageShell("Go vs Rust", "compare", "../", renderComparePage(analysis, "../")))

	// ---- raw data, for auditing / future JS consumption --------------
	writeJSON(filepath.Join(*outDir, "data", "summary.json"), stats)

	fmt.Printf("bench-parser: wrote %d benchmark groups from %d samples to %s\n", len(stats), len(all), *outDir)
	fmt.Printf("  fastest insert:            %s\n", describe(analysis.FastestInsertOverall))
	fmt.Printf("  fastest lookup:            %s\n", describe(analysis.FastestLookupOverall))
	fmt.Printf("  fastest concurrent lookup: %s\n", describe(analysis.FastestConcurrentOverall))
	fmt.Printf("  zero-alloc groups:         %d\n", len(analysis.ZeroAllocGroups))
}

func describe(s *Stat) string {
	if s == nil {
		return "n/a"
	}
	return fmt.Sprintf("%s/%s/%s/%s = %s", s.Key.Runtime, s.Key.Family, s.Key.Variant, s.Key.IPVer, fmtNs(s.MeanNs))
}

func write(dir, name, content string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fatalf("writing %s: %v", path, err)
	}
}

func writeJSON(path string, v interface{}) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fatalf("mkdir for %s: %v", path, err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatalf("marshaling json for %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fatalf("writing %s: %v", path, err)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "bench-parser: "+format+"\n", args...)
	os.Exit(1)
}
