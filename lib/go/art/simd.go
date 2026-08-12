//go:build !simd && !simd_cgo
// +build !simd,!simd_cgo

// simd.go — Scalar baseline for Node16 key lookup.
//
// This file is compiled when NEITHER the "simd" NOR the "simd_cgo" build tag
// is active.  It provides a safe, pure-Go implementation that works on every
// platform and requires no external dependencies.
//
// To use real SIMD acceleration, build with one of:
//   -tags simd       Pure-Go fast path (bytes.IndexByte, good for most cases)
//   -tags simd_cgo   Rust FFI SIMD (AVX2/SSE4.1/NEON — requires shared lib)
//
// See docs/guides/go-simd-rust-ffi.md for the full rationale.

package art

// useSIMD is false in the scalar baseline path.
// Both simd.go and simd_enabled.go set this; the build tags ensure only one
// file is compiled at a time.
var useSIMD = false

// simdFindKey is the scalar fallback implementation.
// It is a tight loop intentionally kept small so the compiler can inline it.
func simdFindKey(keys *[16]byte, target byte, count uint8) int {
	for i := 0; i < int(count); i++ {
		if keys[i] == target {
			return i
		}
	}
	return -1
}
