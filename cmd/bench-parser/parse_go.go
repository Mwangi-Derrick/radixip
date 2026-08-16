package main

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// go test -bench output line, e.g.:
//
//	BenchmarkInsert_IPv4_Uncompressed_5k_Normal-4    20   55645760 ns/op   82564022 B/op   1120068 allocs/op
//	BenchmarkConcurrent_Lookup_IPv4_ART_25k-4  24097062  49.69 ns/op   0 B/op   0 allocs/op
var reGoResult = regexp.MustCompile(
	`^(Benchmark\S+)-(\d+)\s+(\d+)\s+([0-9]+\.?[0-9]*)\s+ns/op(?:\s+([0-9]+)\s+B/op)?(?:\s+([0-9]+)\s+allocs/op)?`,
)

var rePkgLine = regexp.MustCompile(`^pkg:\s+(\S+)`)

// parseGoLog reads a raw `go test -run=NONE -bench=. -benchmem -v` log and
// returns one Sample per benchmark result line, tagged with the GroupKey
// derived from its name and enclosing package. Samples are normalized to
// true per-operation ns before being returned (see classifyGoName).
func parseGoLog(r io.Reader) []classifiedSample {
	var out []classifiedSample
	currentPkg := ""

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if m := rePkgLine.FindStringSubmatch(line); m != nil {
			currentPkg = m[1]
			continue
		}

		m := reGoResult.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		name := m[1]
		ns, _ := strconv.ParseFloat(m[4], 64)
		s := Sample{RawName: name, NsOp: ns}
		if m[5] != "" && m[6] != "" {
			s.HasMem = true
			b, _ := strconv.ParseInt(m[5], 10, 64)
			a, _ := strconv.ParseInt(m[6], 10, 64)
			s.BytesOp = b
			s.AllocsOp = a
		}

		key, divisor, ok := classifyGoName(name, currentPkg)
		if !ok {
			continue
		}
		s.NsOp = s.NsOp / divisor
		if s.HasMem {
			// B/op and allocs/op are also whole-batch totals for the
			// sequential benchmarks; normalize them the same way so
			// "memory per operation" is directly comparable across groups.
			s.BytesOp = int64(float64(s.BytesOp) / divisor)
			s.AllocsOp = int64(float64(s.AllocsOp) / divisor)
		}
		out = append(out, classifiedSample{Key: key, Sample: s})
	}
	return out
}

type classifiedSample struct {
	Key    GroupKey
	Sample Sample
}

// classifyGoName tokenizes a Go benchmark name into a GroupKey.
//
// Observed shapes in this codebase:
//
//	Insert_IPv4_Uncompressed_5k_Normal
//	Lookup_Hit_IPv4_Uncompressed_25k_Normal
//	Lookup_Miss_IPv4_Compressed_50k_Padded
//	Insert_ART_IPv4_10k
//	Lookup_Hit_ART_IPv4_50k
//	Concurrent_Lookup_IPv4_Uncompressed_Normal
//	Concurrent_Lookup_IPv4_ART_25k
//	Tree_Insert / Tree_Match_Hit / Tree_Match_Miss   (lib/go/art package, no IP/variant label)
//
// Go's sequential (non-RunParallel) benchmarks loop over an entire dataset
// (the "5k"/"25k"/"50k"/"10k" suffix) inside a single b.N iteration, so the
// reported ns/op is a WHOLE-BATCH time, not a per-lookup/per-insert time —
// this matches the divisor methodology documented in the project README.
// classifyGoName therefore also returns the divisor needed to convert the
// raw ns/op into a true per-operation figure. RunParallel-based
// Concurrent_Lookup_* benchmarks already report true per-op time (divisor 1).
// The package-level Tree_Insert/Tree_Match_* microbenchmarks carry no size
// suffix at all, so they're left un-normalized (divisor 1) and are treated
// as "core" figures excluded from cross-family per-op rankings — see the
// IPVer == "n/a" handling in aggregate.go / render.go.
func classifyGoName(fullName, pkg string) (GroupKey, float64, bool) {
	name := strings.TrimPrefix(fullName, "Benchmark")
	tokens := strings.Split(name, "_")
	if len(tokens) == 0 {
		return GroupKey{}, 1, false
	}

	k := GroupKey{Runtime: "go", IPVer: "n/a", Variant: "Core"}

	concurrent := false
	sawInsert, sawLookup, sawMatch := false, false, false
	sawHit, sawMiss := false, false
	family := ""

	for _, t := range tokens {
		switch t {
		case "Concurrent":
			concurrent = true
		case "Insert":
			sawInsert = true
		case "Lookup":
			sawLookup = true
		case "Match":
			sawMatch = true
		case "Hit":
			sawHit = true
		case "Miss":
			sawMiss = true
		case "ART":
			family = "art"
		case "Compressed":
			family = "patricia-tree"
		case "Uncompressed":
			family = "binary-tries"
		case "IPv4":
			k.IPVer = "ipv4"
		case "IPv6":
			k.IPVer = "ipv6"
		case "Normal", "Atomic", "Padded", "LockFree":
			k.Variant = t
		case "Tree":
			// lib/go/art package's own tree_test.go microbenchmarks
		default:
			if looksLikeSize(t) {
				k.Size = t
			}
		}
	}

	// Package-level fallback: lib/go/art's Tree_* benchmarks carry no
	// explicit ART/IPv4 tokens but they ARE the ART engine's core tree.
	if strings.HasSuffix(pkg, "/lib/go/art") {
		family = "art"
	}
	if family == "" {
		return GroupKey{}, 1, false
	}
	k.Family = family

	if family == "art" && k.Variant == "Core" {
		k.Variant = "ART"
	}

	switch {
	case sawInsert:
		k.Op = "insert"
	case sawLookup || sawMatch:
		k.Op = "lookup"
		k.Concurrent = concurrent
		switch {
		case sawHit:
			k.SubOp = "hit"
		case sawMiss:
			k.SubOp = "miss"
		}
	default:
		return GroupKey{}, 1, false
	}

	divisor := 1.0
	switch {
	case k.Op == "lookup" && k.Concurrent:
		// RunParallel: b.N already counts individual lookups.
		divisor = 1
	case k.IPVer == "n/a":
		// Core lib/go/art tree microbenchmarks: no documented batch size.
		divisor = 1
	default:
		if n, ok := sizeToCount(k.Size); ok {
			divisor = float64(n)
		}
	}

	// Batch size was only needed to compute the divisor; drop it from the
	// grouping key so repeated runs / differently-sized datasets of the
	// same variant aggregate together instead of rendering as duplicate rows.
	k.Size = ""

	return k, divisor, true
}

func looksLikeSize(t string) bool {
	if t == "" {
		return false
	}
	if strings.HasSuffix(t, "k") {
		_, err := strconv.Atoi(strings.TrimSuffix(t, "k"))
		return err == nil
	}
	_, err := strconv.Atoi(t)
	return err == nil
}

// sizeToCount converts a size token like "5k", "500", "25000" into an
// absolute integer count.
func sizeToCount(tok string) (int64, bool) {
	if tok == "" {
		return 0, false
	}
	if strings.HasSuffix(tok, "k") {
		n, err := strconv.ParseInt(strings.TrimSuffix(tok, "k"), 10, 64)
		if err != nil {
			return 0, false
		}
		return n * 1000, true
	}
	n, err := strconv.ParseInt(tok, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
