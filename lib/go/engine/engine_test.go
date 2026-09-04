package radixip

import (
	"fmt"
	"net"
	"testing"
)

// Dataset helpers - IPv4

func generateCIDRsIPv4(n int) []*net.IPNet {
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

func generateHitIPsIPv4(n int) []net.IP {
	ips := make([]net.IP, 0, n)
	for i := 0; i < n; i++ {
		a := (i / (256 * 256)) % 256
		b := (i / 256) % 256
		c := i % 256
		ips = append(ips, net.ParseIP(fmt.Sprintf("10.%d.%d.%d", a, b, c)))
	}
	return ips
}

func generateMissIPsIPv4(n int) []net.IP {
	ips := make([]net.IP, 0, n)
	for i := 0; i < n; i++ {
		a := (i / (256 * 256)) % 256
		b := (i / 256) % 256
		c := i % 256
		ips = append(ips, net.ParseIP(fmt.Sprintf("172.%d.%d.%d", a, b, c)))
	}
	return ips
}

// Dataset helpers - IPv6

func generateCIDRsIPv6(n int) []*net.IPNet {
	cidrs := make([]*net.IPNet, 0, n)
	for i := 0; i < n; i++ {
		low := uint16(i % 65536)
		high := uint16((i / 65536) % 65536)
		byte3 := uint8((i / (65536 * 65536)) % 256)
		_, ipnet, _ := net.ParseCIDR(fmt.Sprintf("fd%02x:%04x:%04x::/64", byte3, high, low))
		cidrs = append(cidrs, ipnet)
	}
	return cidrs
}

func generateHitIPsIPv6(n int) []net.IP {
	ips := make([]net.IP, 0, n)
	for i := 0; i < n; i++ {
		low := uint16(i % 65536)
		high := uint16((i / 65536) % 65536)
		byte3 := uint8((i / (65536 * 65536)) % 256)
		ips = append(ips, net.ParseIP(fmt.Sprintf("fd%02x:%04x:%04x:%04x:dead:beef:cafe:feed",
			byte3, high, low, uint16(i%256))))
	}
	return ips
}

func generateMissIPsIPv6(n int) []net.IP {
	ips := make([]net.IP, 0, n)
	for i := 0; i < n; i++ {
		low := uint16(i % 65536)
		high := uint16((i / 65536) % 65536)
		ips = append(ips, net.ParseIP(fmt.Sprintf("2001:db8:%04x:%04x:dead:beef:cafe:feed",
			high, low)))
	}
	return ips
}

// Helpers

func buildEngine(n int, compressed bool, ipv6 bool) RadixEngine {
	e := NewEngineWrapperWithTree(EngineConcurrent, AtomicTrieNode, compressed)
	meta := Metadata{Value: "bench", Attributes: map[string]string{"type": "benchmark"}}

	var cidrs []*net.IPNet
	if ipv6 {
		cidrs = generateCIDRsIPv6(n)
	} else {
		cidrs = generateCIDRsIPv4(n)
	}

	for _, cidr := range cidrs {
		_ = e.Insert(cidr, meta)
	}
	return RadixEngine(e.engine)
}

func buildEngineWithVariant(n int, compressed bool, nodeVariant NodeVariant, ipv6 bool) RadixEngine {
	e := NewEngineWrapperWithTree(EngineConcurrent, nodeVariant, compressed)
	meta := Metadata{Value: "bench", Attributes: map[string]string{"type": "benchmark"}}

	var cidrs []*net.IPNet
	if ipv6 {
		cidrs = generateCIDRsIPv6(n)
	} else {
		cidrs = generateCIDRsIPv4(n)
	}

	for _, cidr := range cidrs {
		_ = e.Insert(cidr, meta)
	}
	return RadixEngine(e.engine)
}

func buildEngineART(n int, ipv6 bool) RadixEngine {
	e := NewARTEngineAdapter()
	meta := Metadata{Value: "bench", Attributes: map[string]string{"type": "benchmark"}}

	var cidrs []*net.IPNet
	if ipv6 {
		cidrs = generateCIDRsIPv6(n)
	} else {
		cidrs = generateCIDRsIPv4(n)
	}

	for _, cidr := range cidrs {
		_ = e.Insert(cidr, meta)
	}
	return RadixEngine(e)
}

var (
	GlobalResult *Metadata
)

// Insert Benchmarks

// IPv4 Insert Benchmarks
func BenchmarkInsert_IPv4_Uncompressed_5k_Normal(b *testing.B) {
	cidrs := generateCIDRsIPv4(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NormalTrieNode, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv4_Uncompressed_5k_Atomic(b *testing.B) {
	cidrs := generateCIDRsIPv4(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, AtomicTrieNode, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv4_Uncompressed_5k_Padded(b *testing.B) {
	cidrs := generateCIDRsIPv4(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, PaddedTrieNode, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv4_Uncompressed_5k_LockFree(b *testing.B) {
	cidrs := generateCIDRsIPv4(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, LockFreeTrieNode, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv4_Compressed_5k_Normal(b *testing.B) {
	cidrs := generateCIDRsIPv4(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NormalRadixNode, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv4_Compressed_5k_Atomic(b *testing.B) {
	cidrs := generateCIDRsIPv4(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, AtomicRadixNode, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv4_Compressed_5k_Padded(b *testing.B) {
	cidrs := generateCIDRsIPv4(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, PaddedRadixNode, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv4_Compressed_5k_LockFree(b *testing.B) {
	cidrs := generateCIDRsIPv4(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, LockFreeRadixNode, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

// IPv6 Insert Benchmarks
func BenchmarkInsert_IPv6_Uncompressed_5k_Normal(b *testing.B) {
	cidrs := generateCIDRsIPv6(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NormalTrieNode, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv6_Uncompressed_5k_Atomic(b *testing.B) {
	cidrs := generateCIDRsIPv6(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, AtomicTrieNode, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv6_Uncompressed_5k_Padded(b *testing.B) {
	cidrs := generateCIDRsIPv6(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, PaddedTrieNode, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv6_Uncompressed_5k_LockFree(b *testing.B) {
	cidrs := generateCIDRsIPv6(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, LockFreeTrieNode, false)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv6_Compressed_5k_Normal(b *testing.B) {
	cidrs := generateCIDRsIPv6(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, NormalRadixNode, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv6_Compressed_5k_Atomic(b *testing.B) {
	cidrs := generateCIDRsIPv6(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, AtomicRadixNode, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv6_Compressed_5k_Padded(b *testing.B) {
	cidrs := generateCIDRsIPv6(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, PaddedRadixNode, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkInsert_IPv6_Compressed_5k_LockFree(b *testing.B) {
	cidrs := generateCIDRsIPv6(5_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	for b.Loop() {
		e := NewEngineWrapperWithTree(EngineConcurrent, LockFreeRadixNode, true)
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

// Lookup Benchmarks - Hit (Uncompressed) - IPv4

func BenchmarkLookup_Hit_IPv4_Uncompressed_25k_Normal(b *testing.B) {
	e := buildEngineWithVariant(25_000, false, NormalTrieNode, false)
	ips := generateHitIPsIPv4(25_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv4_Uncompressed_25k_Atomic(b *testing.B) {
	e := buildEngineWithVariant(25_000, false, AtomicTrieNode, false)
	ips := generateHitIPsIPv4(25_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv4_Uncompressed_25k_Padded(b *testing.B) {
	e := buildEngineWithVariant(25_000, false, PaddedTrieNode, false)
	ips := generateHitIPsIPv4(25_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv4_Uncompressed_25k_LockFree(b *testing.B) {
	e := buildEngineWithVariant(25_000, false, LockFreeTrieNode, false)
	ips := generateHitIPsIPv4(25_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// Lookup Benchmarks - Hit (Uncompressed) - IPv6

func BenchmarkLookup_Hit_IPv6_Uncompressed_25k_Normal(b *testing.B) {
	e := buildEngineWithVariant(25_000, false, NormalTrieNode, true)
	ips := generateHitIPsIPv6(25_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv6_Uncompressed_25k_Atomic(b *testing.B) {
	e := buildEngineWithVariant(25_000, false, AtomicTrieNode, true)
	ips := generateHitIPsIPv6(25_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv6_Uncompressed_25k_Padded(b *testing.B) {
	e := buildEngineWithVariant(25_000, false, PaddedTrieNode, true)
	ips := generateHitIPsIPv6(25_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv6_Uncompressed_25k_LockFree(b *testing.B) {
	e := buildEngineWithVariant(25_000, false, LockFreeTrieNode, true)
	ips := generateHitIPsIPv6(25_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// Lookup Benchmarks - Hit (Compressed) - IPv4

func BenchmarkLookup_Hit_IPv4_Compressed_50k_Normal(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NormalRadixNode, false)
	ips := generateHitIPsIPv4(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv4_Compressed_50k_Atomic(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, AtomicRadixNode, false)
	ips := generateHitIPsIPv4(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv4_Compressed_50k_Padded(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, PaddedRadixNode, false)
	ips := generateHitIPsIPv4(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv4_Compressed_50k_LockFree(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, LockFreeRadixNode, false)
	ips := generateHitIPsIPv4(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// Lookup Benchmarks - Hit (Compressed) - IPv6

func BenchmarkLookup_Hit_IPv6_Compressed_50k_Normal(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NormalRadixNode, true)
	ips := generateHitIPsIPv6(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv6_Compressed_50k_Atomic(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, AtomicRadixNode, true)
	ips := generateHitIPsIPv6(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv6_Compressed_50k_Padded(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, PaddedRadixNode, true)
	ips := generateHitIPsIPv6(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Hit_IPv6_Compressed_50k_LockFree(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, LockFreeRadixNode, true)
	ips := generateHitIPsIPv6(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// Lookup Benchmarks - Miss - IPv4

func BenchmarkLookup_Miss_IPv4_Compressed_50k_Normal(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NormalRadixNode, false)
	ips := generateMissIPsIPv4(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_IPv4_Compressed_50k_Atomic(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, AtomicRadixNode, false)
	ips := generateMissIPsIPv4(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_IPv4_Compressed_50k_Padded(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, PaddedRadixNode, false)
	ips := generateMissIPsIPv4(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_IPv4_Compressed_50k_LockFree(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, LockFreeRadixNode, false)
	ips := generateMissIPsIPv4(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// Lookup Benchmarks - Miss - IPv6

func BenchmarkLookup_Miss_IPv6_Compressed_50k_Normal(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, NormalRadixNode, true)
	ips := generateMissIPsIPv6(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_IPv6_Compressed_50k_Atomic(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, AtomicRadixNode, true)
	ips := generateMissIPsIPv6(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_IPv6_Compressed_50k_Padded(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, PaddedRadixNode, true)
	ips := generateMissIPsIPv6(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_IPv6_Compressed_50k_LockFree(b *testing.B) {
	e := buildEngineWithVariant(50_000, true, LockFreeRadixNode, true)
	ips := generateMissIPsIPv6(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// ART Engine Benchmarks - IPv4

func BenchmarkInsert_ART_IPv4_10k(b *testing.B) {
	cidrs := generateCIDRsIPv4(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		e := NewARTEngineAdapter()
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkLookup_Hit_ART_IPv4_50k(b *testing.B) {
	e := NewARTEngineAdapter()
	cidrs := generateCIDRsIPv4(50_000)
	meta := Metadata{Value: "bench"}
	for _, cidr := range cidrs {
		_ = e.Insert(cidr, meta)
	}
	ips := generateHitIPsIPv4(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_ART_IPv4_50k(b *testing.B) {
	e := NewARTEngineAdapter()
	cidrs := generateCIDRsIPv4(50_000)
	meta := Metadata{Value: "bench"}
	for _, cidr := range cidrs {
		_ = e.Insert(cidr, meta)
	}
	ips := generateMissIPsIPv4(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// ART Engine Benchmarks - IPv6

func BenchmarkInsert_ART_IPv6_10k(b *testing.B) {
	cidrs := generateCIDRsIPv6(10_000)
	meta := Metadata{Value: "bench"}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		e := NewARTEngineAdapter()
		for _, cidr := range cidrs {
			_ = e.Insert(cidr, meta)
		}
	}
}

func BenchmarkLookup_Hit_ART_IPv6_50k(b *testing.B) {
	e := NewARTEngineAdapter()
	cidrs := generateCIDRsIPv6(50_000)
	meta := Metadata{Value: "bench"}
	for _, cidr := range cidrs {
		_ = e.Insert(cidr, meta)
	}
	ips := generateHitIPsIPv6(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

func BenchmarkLookup_Miss_ART_IPv6_50k(b *testing.B) {
	e := NewARTEngineAdapter()
	cidrs := generateCIDRsIPv6(50_000)
	meta := Metadata{Value: "bench"}
	for _, cidr := range cidrs {
		_ = e.Insert(cidr, meta)
	}
	ips := generateMissIPsIPv6(50_000)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			GlobalResult = e.Lookup(ip)
		}
	}
}

// Concurrent Lookup Benchmarks - IPv4 (to match Rust)

func BenchmarkConcurrent_Lookup_IPv4_Uncompressed_Normal(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, false, NormalTrieNode, false)
	ips := generateHitIPsIPv4(n)
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

func BenchmarkConcurrent_Lookup_IPv4_Uncompressed_Atomic(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, false, AtomicTrieNode, false)
	ips := generateHitIPsIPv4(n)
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

func BenchmarkConcurrent_Lookup_IPv4_Uncompressed_Padded(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, false, PaddedTrieNode, false)
	ips := generateHitIPsIPv4(n)
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

func BenchmarkConcurrent_Lookup_IPv4_Uncompressed_LockFree(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, false, LockFreeTrieNode, false)
	ips := generateHitIPsIPv4(n)
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

func BenchmarkConcurrent_Lookup_IPv4_Compressed_Normal(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, true, NormalRadixNode, false)
	ips := generateHitIPsIPv4(n)
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

func BenchmarkConcurrent_Lookup_IPv4_Compressed_Atomic(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, true, AtomicRadixNode, false)
	ips := generateHitIPsIPv4(n)
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

func BenchmarkConcurrent_Lookup_IPv4_Compressed_Padded(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, true, PaddedRadixNode, false)
	ips := generateHitIPsIPv4(n)
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

func BenchmarkConcurrent_Lookup_IPv4_Compressed_LockFree(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, true, LockFreeRadixNode, false)
	ips := generateHitIPsIPv4(n)
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

func BenchmarkConcurrent_Lookup_IPv4_ART_25k(b *testing.B) {
	const n = 25_000
	e := buildEngineART(n, false)
	ips := generateHitIPsIPv4(n)
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

// Concurrent Lookup Benchmarks - IPv6 (to match Rust)

func BenchmarkConcurrent_Lookup_IPv6_Uncompressed_Normal(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, false, NormalTrieNode, true)
	ips := generateHitIPsIPv6(n)
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

func BenchmarkConcurrent_Lookup_IPv6_Uncompressed_Atomic(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, false, AtomicTrieNode, true)
	ips := generateHitIPsIPv6(n)
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

func BenchmarkConcurrent_Lookup_IPv6_Uncompressed_Padded(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, false, PaddedTrieNode, true)
	ips := generateHitIPsIPv6(n)
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

func BenchmarkConcurrent_Lookup_IPv6_Uncompressed_LockFree(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, false, LockFreeTrieNode, true)
	ips := generateHitIPsIPv6(n)
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

func BenchmarkConcurrent_Lookup_IPv6_Compressed_Normal(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, true, NormalRadixNode, true)
	ips := generateHitIPsIPv6(n)
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

func BenchmarkConcurrent_Lookup_IPv6_Compressed_Atomic(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, true, AtomicRadixNode, true)
	ips := generateHitIPsIPv6(n)
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

func BenchmarkConcurrent_Lookup_IPv6_Compressed_Padded(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, true, PaddedRadixNode, true)
	ips := generateHitIPsIPv6(n)
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

func BenchmarkConcurrent_Lookup_IPv6_Compressed_LockFree(b *testing.B) {
	const n = 25_000
	e := buildEngineWithVariant(n, true, LockFreeRadixNode, true)
	ips := generateHitIPsIPv6(n)
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

func BenchmarkConcurrent_Lookup_IPv6_ART_25k(b *testing.B) {
	const n = 25_000
	e := buildEngineART(n, true)
	ips := generateHitIPsIPv6(n)
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
