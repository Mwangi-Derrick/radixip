package main

import "fmt"

// Sample is one raw benchmark observation (one line of go test -bench or
// one "test ... bench:" line from cargo bench -- --output-format bencher).
type Sample struct {
	RawName  string
	NsOp     float64
	BytesOp  int64
	AllocsOp int64
	HasMem   bool // Rust bencher output never reports B/op or allocs/op
}

// GroupKey uniquely classifies a family of benchmarks so that repeated
// -count=10 samples (Go) or the single Criterion/bencher sample (Rust)
// land in the same bucket for averaging.
type GroupKey struct {
	Runtime    string // "go" | "rust"
	Family     string // "binary-tries" | "patricia-tree" | "art"
	Op         string // "insert" | "lookup"
	SubOp      string // "hit" | "miss" | "" (concurrent / plain)
	Concurrent bool
	IPVer      string // "ipv4" | "ipv6" | "n/a"
	Variant    string // "Normal" | "Atomic" | "Padded" | "LockFree" | "ART" | "Core"
	Size       string // informational: "500", "5k", "25k", "4_threads"
}

func (k GroupKey) ID() string {
	return fmt.Sprintf("%s|%s|%s|%s|%v|%s|%s|%s",
		k.Runtime, k.Family, k.Op, k.SubOp, k.Concurrent, k.IPVer, k.Variant, k.Size)
}

// Stat is the aggregated, CI-report-ready view of a Group.
type Stat struct {
	Key          GroupKey
	Count        int
	MeanNs       float64
	MinNs        float64
	MaxNs        float64
	OpsPerSec    float64
	MeanBytesOp  float64
	MeanAllocsOp float64
	HasMem       bool
	ZeroAlloc    bool
	SourceLines  []string
}

func familyLabel(f string) string {
	switch f {
	case "binary-tries":
		return "Binary Trie (Uncompressed)"
	case "patricia-tree":
		return "Patricia / Radix Tree (Compressed)"
	case "art":
		return "Adaptive Radix Tree (ART)"
	}
	return f
}

func familySlugTitle(f string) string {
	switch f {
	case "binary-tries":
		return "Binary Tries"
	case "patricia-tree":
		return "Patricia Trees"
	case "art":
		return "ART (Adaptive Radix Tree)"
	}
	return f
}

func opLabel(k GroupKey) string {
	switch {
	case k.Op == "insert":
		return "Insert"
	case k.Op == "lookup" && k.Concurrent:
		return "Concurrent Lookup"
	case k.Op == "lookup" && k.SubOp == "hit":
		return "Lookup (Hit)"
	case k.Op == "lookup" && k.SubOp == "miss":
		return "Lookup (Miss)"
	case k.Op == "lookup":
		return "Lookup"
	}
	return k.Op
}

func ipverLabel(v string) string {
	switch v {
	case "ipv4":
		return "IPv4"
	case "ipv6":
		return "IPv6"
	default:
		return "N/A"
	}
}

func runtimeLabel(r string) string {
	if r == "go" {
		return "Go"
	}
	return "Rust"
}
