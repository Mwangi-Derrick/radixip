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

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkInsert_Uncompressed_10k(b *testing.B) {
	cidrs := generateCIDRs(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := NewEngineWrapperWithTree(EngineConcurrent, NodeAtomic, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_Compressed_10k(b *testing.B) {
	cidrs := generateCIDRs(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := NewEngineWrapperWithTree(EngineConcurrent, NodeAtomic, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkLookup_Hit_Uncompressed_50k(b *testing.B) {
	e := buildEngine(50_000, false)
	ips := generateHitIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, ip := range ips {
			_ = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_Compressed_50k(b *testing.B) {
	e := buildEngine(50_000, true)
	ips := generateHitIPs(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, ip := range ips {
			_ = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_Uncompressed_50k(b *testing.B) {
	e := buildEngine(50_000, false)
	ips := generateMissIPs(50_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ip := range ips {
			_ = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_Compressed_50k(b *testing.B) {
	e := buildEngine(50_000, true)
	ips := generateMissIPs(50_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ip := range ips {
			_ = e.Lookup(ip)
		}
	}
}

func BenchmarkConcurrent_Lookup_Compressed(b *testing.B) {
	const n = 50_000
	e := buildEngine(n, true)
	ips := generateHitIPs(n)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = e.Lookup(ips[i%n])
			i++
		}
	})
}
