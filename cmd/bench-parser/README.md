# bench-parser

A small, dependency-free Go CLI that turns raw `go test -bench` and
`cargo bench` output into a static, multi-page benchmark dashboard suitable
for GitHub Pages.

It replaces hand-editing `docs/benchmarks/index.html` after every CI run:
the workflow now runs this tool against the two fresh log files and commits
the generated site.

## What it does

1. **Parses** two log formats:
   - Go: `go test -run=NONE -bench=. -benchmem -count=10 -v -tags simd_cgo ./...`
   - Rust: `cargo bench --bench lookup_bench -- --output-format bencher`
2. **Classifies** every benchmark result by:
   - runtime (`go` / `rust`)
   - tree family (`binary-tries` / `patricia-tree` / `art`)
   - operation (`insert` / `lookup`, with `hit`/`miss`/concurrent sub-kinds)
   - IP version (`ipv4` / `ipv6`)
   - concurrency variant (`Normal` / `Atomic` / `Padded` / `LockFree` / `ART`)
3. **Normalizes** every figure to a true per-operation latency. Go's
   sequential benchmarks and Rust's `insert` benchmarks report *whole-batch*
   time (e.g. one `ns/op` for inserting an entire 5,000-prefix batch); this
   tool divides by the batch size encoded in the benchmark name so every
   number on the dashboard is directly comparable. Go's `RunParallel`
   concurrent-lookup benchmarks and the batch size baked into their names
   are handled per the divisor rules documented inline in
   `parse_go.go` / `parse_rust.go` (which mirror the methodology already
   described in the project README).
4. **Aggregates** repeated `-count=10` samples per benchmark into mean /
   min / max / throughput / memory figures.
5. **Renders** a static site — one landing page, one page per runtime, one
   page per (runtime × tree family), and a head-to-head comparison page —
   instead of a single endlessly-scrolling page.
6. **Writes** `data/summary.json` with every aggregated stat, for auditing
   or future client-side use.

## Site layout produced

```
docs/benchmarks/
├── index.html            Overview: headline cards + links to everything
├── go/index.html         Go runtime summary + best-variant table
├── go/binary-tries/index.html
├── go/patricia-tree/index.html
├── go/art/index.html
├── rust/index.html       Rust runtime summary + best-variant table
├── rust/binary-tries/index.html
├── rust/patricia-tree/index.html
├── rust/art/index.html
├── compare/index.html    Go vs Rust, per family, per IP version
└── data/summary.json     Raw aggregated stats
```

`dev/bench/` (the interactive commit-history charts from
`benchmark-action/github-action-benchmark`) is left untouched — the nav bar
on every page links out to it.

## Usage

```sh
go build -o bin/bench-parser ./cmd/bench-parser

./bin/bench-parser \
  -go-log go_bench_results.txt \
  -rust-log rust_bench_results.txt \
  -out docs/benchmarks
```

Either `-go-log` or `-rust-log` may be omitted if that run's log isn't
available; the other language's pages are still generated.

## Extending the classifier

If you add a new tree implementation, concurrency variant, or benchmark
naming shape, update the token-matching logic in `classifyGoName`
(`parse_go.go`) or `classifyRustName` (`parse_rust.go`) — both are small,
explicit switch statements over name tokens rather than a single fragile
regex, specifically so new benchmark shapes are a two-line change instead
of a rewrite.
