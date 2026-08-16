package main

import (
	"bufio"
	"io"
	"os"
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

// MemoryStats holds the extracted metrics from a DHAT text dump
type MemoryStats struct {
	TotalBytes       int64
	TotalAllocations int64
}

// ParseDhatText reads the stderr text file generated from a DHAT-profiled execution
func ParseDhatText(filePath string) (MemoryStats, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return MemoryStats{}, err
	}
	defer file.Close()

	var stats MemoryStats
	scanner := bufio.NewScanner(file)
	// Regexp to match: "dhat: Total: 1,256 bytes in 6 blocks"
	re := regexp.MustCompile(`dhat: Total:\s+([\d,]+)\s+bytes\s+in\s+([\d,]+)\s+blocks`)

	for scanner.Scan() {
		line := scanner.Text()
		if matches := re.FindStringSubmatch(line); matches != nil {
			bytesStr := strings.ReplaceAll(matches[1], ",", "")
			blocksStr := strings.ReplaceAll(matches[2], ",", "")
			stats.TotalBytes, _ = strconv.ParseInt(bytesStr, 10, 64)
			stats.TotalAllocations, _ = strconv.ParseInt(blocksStr, 10, 64)
			break
		}
	}
	return stats, scanner.Err()
}

// InjectDHATStats merges the parsed DHAT stats back into the raw samples.
// This requires the user to specify a GroupKey ID string mapping for the stats.
func InjectDHATStats(all []classifiedSample, dhatFile string, targetKeyID string) {
	dhatStats, err := ParseDhatText(dhatFile)
	if err != nil {
		return // Gracefully skip if file doesn't exist
	}
	for i := range all {
		if all[i].Key.ID() == targetKeyID {
			all[i].Sample.HasMem = true
			all[i].Sample.BytesOp = dhatStats.TotalBytes
			all[i].Sample.AllocsOp = dhatStats.TotalAllocations
		}
	}
}
