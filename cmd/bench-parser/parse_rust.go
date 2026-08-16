package main

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// libtest/bencher-format line, e.g.:
//
//	test insert/ipv4/compressed/ART/NormalRadixNode/500 ... bench: 73330 ns/iter (+/- 206)
//	test concurrent_lookup/art/ipv4/4_threads/ART ... bench: 7722768 ns/iter (+/- 52840)
var reRustResult = regexp.MustCompile(`^test\s+(\S+)\s+\.\.\.\s+bench:\s+([0-9,]+)\s+ns/iter`)

// concurrentLookupsPerThread is the fixed per-thread lookup count used by
// the Rust concurrent-lookup benchmarks (see benches/lookup_bench.rs and
// the project README's benchmark methodology section: "Rust concurrent
// lookup results divide ns/iter by 4 * 25,000 = 100,000 lookups").
const concurrentLookupsPerThread = 25000

// parseRustLog reads a raw `cargo bench -- --output-format bencher` log and
// returns one Sample per benchmark result line, normalized to true
// per-operation ns (see classifyRustName).
func parseRustLog(r io.Reader) []classifiedSample {
	var out []classifiedSample

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		m := reRustResult.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		ns, _ := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", ""), 64)

		key, divisor, ok := classifyRustName(name)
		if !ok {
			continue
		}
		out = append(out, classifiedSample{
			Key:    key,
			Sample: Sample{RawName: name, NsOp: ns / divisor, HasMem: false},
		})
	}
	return out
}

// classifyRustName tokenizes a slash-separated Criterion/bencher benchmark
// path into a GroupKey, plus the divisor needed to convert the whole-batch
// Criterion timing into a true per-operation figure. Observed shapes:
//
//	insert/ipv4/uncompressed/NormalTrieNode/500       (batch of 500 inserts)
//	insert/ipv4/compressed/NormalRadixNode/500
//	insert/ipv4/compressed/ART/NormalRadixNode/500
//	concurrent_lookup/compressed/ipv4/4_threads/NormalRadixNode  (4 * 25,000 lookups)
//	concurrent_lookup/art/ipv4/4_threads/ART
//	concurrent_lookup/uncompressed/ipv4/4_threads/NormalTrieNode
func classifyRustName(name string) (GroupKey, float64, bool) {
	tokens := strings.Split(name, "/")
	if len(tokens) == 0 {
		return GroupKey{}, 1, false
	}

	k := GroupKey{Runtime: "rust", IPVer: "n/a"}

	switch tokens[0] {
	case "insert":
		k.Op = "insert"
	case "lookup":
		k.Op = "lookup"
	case "concurrent_lookup":
		k.Op = "lookup"
		k.Concurrent = true
	default:
		return GroupKey{}, 1, false
	}

	family := ""
	variantToken := ""
	threads := int64(0)

	for _, t := range tokens[1:] {
		switch {
		case t == "ipv4":
			k.IPVer = "ipv4"
		case t == "ipv6":
			k.IPVer = "ipv6"
		case t == "art" || t == "ART":
			family = "art"
			if t == "ART" && variantToken == "" {
				variantToken = "ART"
			}
		case t == "compressed":
			if family == "" {
				family = "patricia-tree"
			}
		case t == "uncompressed":
			if family == "" {
				family = "binary-tries"
			}
		case strings.HasSuffix(t, "TrieNode"):
			variantToken = strings.TrimSuffix(t, "TrieNode")
		case strings.HasSuffix(t, "RadixNode"):
			variantToken = strings.TrimSuffix(t, "RadixNode")
		case strings.HasSuffix(t, "_threads"):
			k.Size = t
			if n, err := strconv.ParseInt(strings.TrimSuffix(t, "_threads"), 10, 64); err == nil {
				threads = n
			}
		default:
			if looksLikeSize(t) {
				k.Size = t
			}
		}
	}

	if family == "" {
		return GroupKey{}, 1, false
	}
	k.Family = family
	if variantToken == "" {
		variantToken = "Core"
	}
	k.Variant = variantToken

	divisor := 1.0
	switch {
	case k.Op == "insert" || (k.Op == "lookup" && !k.Concurrent):
		if n, ok := sizeToCount(k.Size); ok {
			divisor = float64(n)
		}
	case k.Op == "lookup" && k.Concurrent && threads > 0:
		divisor = float64(threads) * concurrentLookupsPerThread
	}

	// As with Go, drop the batch-size token from the grouping key once
	// it's done its job, so differently-sized runs of the same variant
	// aggregate together instead of rendering as duplicate rows.
	k.Size = ""

	return k, divisor, true
}
