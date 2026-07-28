package radixip

import (
	"fmt"
	"net"
	"testing"
)

// ---------------------------------------------------------------------------
// Dataset helpers
// ---------------------------------------------------------------------------

func generateCIDRs(n int) []*net.IPNet {
	cidrs := make([]*net.IPNet, 0, n)
	for i := 0; i < n; i++ {
		a := (i / (256 * 256)) % 256
		b := (i / 256) % 256
		c := i % 256
		_, ipnet, _ := net.ParseCIDR(fmt.Sprintf("10.%d.%d.%d/24", a, b, c))
		cidrs = append(cidrs, ipnet)
	}
	return cidrs
}

func generateHitIPs(n int) []net.IP {
	ips := make([]net.IP, 0, n)
	for i := 0; i < n; i++ {
		a := (i / (256 * 256)) % 256
		b := (i / 256) % 256
		c := i % 256
		ips = append(ips, net.ParseIP(fmt.Sprintf("10.%d.%d.%d", a, b, c)))
	}
	return ips
}

func generateMissIPs(n int) []net.IP {
	ips := make([]net.IP, 0, n)
	for i := 0; i < n; i++ {
		a := (i / (256 * 256)) % 256
		b := (i / 256) % 256
		c := i % 256
		ips = append(ips, net.ParseIP(fmt.Sprintf("172.%d.%d.%d", a, b, c)))
	}
	return ips
}

func buildEngine(n int, compressed bool) RadixEngine {
	e := NewEngineWrapperWithTree(EngineConcurrent, NodeAtomic, compressed)
	meta := Metadata{Value: "bench", Attributes: map[string]string{"type": "benchmark"}}
	for _, cidr := range generateCIDRs(n) {
		_ = e.Insert(cidr, meta)
	}
	return RadixEngine(e.engine)
}

func buildEngineWithVariant(n int, compressed bool, nodeVariant NodeVariant) RadixEngine {
	e := NewEngineWrapperWithTree(EngineConcurrent, nodeVariant, compressed)
	meta := Metadata{Value: "bench", Attributes: map[string]string{"type": "benchmark"}}
	for _, cidr := range generateCIDRs(n) {
		_ = e.Insert(cidr, meta)
	}
	return RadixEngine(e.engine)
}

var (
	GlobalResult *Metadata
)

// ---------------------------------------------------------------------------
// Insert Benchmarks (unchanged)
// ---------------------------------------------------------------------------

func BenchmarkInsert_Uncompressed_10k_Normal(b *testing.B) {
	cidrs := generateCIDRs(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NodeNormal, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_Uncompressed_10k_Atomic(b *testing.B) {
	cidrs := generateCIDRs(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NodeAtomic, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_Uncompressed_10k_Padded(b *testing.B) {
	cidrs := generateCIDRs(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NodePadded, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_Uncompressed_10k_LockFree(b *testing.B) {
	cidrs := generateCIDRs(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NodeLockFree, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_Compressed_10k_Normal(b *testing.B) {
	cidrs := generateCIDRs(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NodeCompressedNormal, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_Compressed_10k_Atomic(b *testing.B) {
	cidrs := generateCIDRs(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NodeCompressedAtomic, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_Compressed_10k_Padded(b *testing.B) {
	cidrs := generateCIDRs(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NodeCompressedPadded, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_Compressed_10k_LockFree(b *testing.B) {
	cidrs := generateCIDRs(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NodeCompressedLockFree, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

// ---------------------------------------------------------------------------
// Lookup Benchmarks - Hit (Uncompressed)
// ---------------------------------------------------------------------------

func BenchmarkLookup_Hit_Uncompressed_50k_Normal(b *testing.B) {
	e := buildEngineWithVariant(50_000, false, NodeNormal)
	ips := generateHitIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_Uncompressed_50k_Atomic(b *testing.B) {
	e := buildEngineWithVariant(50_000, false, NodeAtomic)
	ips := generateHitIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_Uncompressed_50k_Padded(b *testing.B) {
	e := buildEngineWithVariant(50_000, false, NodePadded)
	ips := generateHitIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_Uncompressed_50k_LockFree(b *testing.B) {
	e := buildEngineWithVariant(50_000, false, NodeLockFree)
	ips := generateHitIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// ---------------------------------------------------------------------------
// Lookup Benchmarks - Miss (Uncompressed)
// ---------------------------------------------------------------------------

func BenchmarkLookup_Miss_Uncompressed_50k_Normal(b *testing.B) {
	e := buildEngineWithVariant(50_000, false, NodeNormal)
	ips := generateMissIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_Uncompressed_50k_Atomic(b *testing.B) {
	e := buildEngineWithVariant(50_000, false, NodeAtomic)
	ips := generateMissIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_Uncompressed_50k_Padded(b *testing.B) {
	e := buildEngineWithVariant(50_000, false, NodePadded)
	ips := generateMissIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_Uncompressed_50k_LockFree(b *testing.B) {
	e := buildEngineWithVariant(50_000, false, NodeLockFree)
	ips := generateMissIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// ---------------------------------------------------------------------------
// Lookup Benchmarks - Hit (Compressed)
// ---------------------------------------------------------------------------

func BenchmarkLookup_Hit_Compressed_50k_Normal(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NodeCompressedNormal)
	ips := generateHitIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_Compressed_50k_Atomic(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NodeCompressedAtomic)
	ips := generateHitIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_Compressed_50k_Padded(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NodeCompressedPadded)
	ips := generateHitIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_Compressed_50k_LockFree(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NodeCompressedLockFree)
	ips := generateHitIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// ---------------------------------------------------------------------------
// Lookup Benchmarks - Miss (Compressed)
// ---------------------------------------------------------------------------

func BenchmarkLookup_Miss_Compressed_50k_Normal(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NodeCompressedNormal)
	ips := generateMissIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_Compressed_50k_Atomic(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NodeCompressedAtomic)
	ips := generateMissIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_Compressed_50k_Padded(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NodeCompressedPadded)
	ips := generateMissIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_Compressed_50k_LockFree(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NodeCompressedLockFree)
	ips := generateMissIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrent Lookup Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkConcurrent_Lookup_Uncompressed(b *testing.B) {
	const n = 50_000
	e := buildEngine(n, false)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			GlobalResult = e.Lookup(ips[i%n])
			i++
		}
	})
}

func BenchmarkConcurrent_Lookup_Compressed(b *testing.B) {
	const n = 50_000
	e := buildEngine(n, true)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			GlobalResult = e.Lookup(ips[i%n])
			i++
		}
	})
}

// ---------------------------------------------------------------------------
// Concurrent Lookup Benchmarks - Per Variant (to match Rust)
// ---------------------------------------------------------------------------

func BenchmarkConcurrent_Lookup_Uncompressed_Normal(b *testing.B) {
	const n = 50_000
	e := buildEngineWithVariant(n, false, NodeNormal)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			GlobalResult = e.Lookup(ips[i%n])
			i++
		}
	})
}

func BenchmarkConcurrent_Lookup_Uncompressed_Atomic(b *testing.B) {
	const n = 50_000
	e := buildEngineWithVariant(n, false, NodeAtomic)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			GlobalResult = e.Lookup(ips[i%n])
			i++
		}
	})
}

func BenchmarkConcurrent_Lookup_Uncompressed_Padded(b *testing.B) {
	const n = 50_000
	e := buildEngineWithVariant(n, false, NodePadded)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			GlobalResult = e.Lookup(ips[i%n])
			i++
		}
	})
}

func BenchmarkConcurrent_Lookup_Uncompressed_LockFree(b *testing.B) {
	const n = 50_000
	e := buildEngineWithVariant(n, false, NodeLockFree)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			GlobalResult = e.Lookup(ips[i%n])
			i++
		}
	})
}

func BenchmarkConcurrent_Lookup_Compressed_Normal(b *testing.B) {
	const n = 50_000
	e := buildEngineWithVariant(n, true, NodeCompressedNormal)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			GlobalResult = e.Lookup(ips[i%n])
			i++
		}
	})
}

func BenchmarkConcurrent_Lookup_Compressed_Atomic(b *testing.B) {
	const n = 50_000
	e := buildEngineWithVariant(n, true, NodeCompressedAtomic)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			GlobalResult = e.Lookup(ips[i%n])
			i++
		}
	})
}

func BenchmarkConcurrent_Lookup_Compressed_Padded(b *testing.B) {
	const n = 50_000
	e := buildEngineWithVariant(n, true, NodeCompressedPadded)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			GlobalResult = e.Lookup(ips[i%n])
			i++
		}
	})
}

func BenchmarkConcurrent_Lookup_Compressed_LockFree(b *testing.B) {
	const n = 50_000
	e := buildEngineWithVariant(n, true, NodeCompressedLockFree)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			GlobalResult = e.Lookup(ips[i%n])
			i++
		}
	})
}
