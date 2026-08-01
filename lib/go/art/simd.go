package art

// Minimal SIMD shim for Node16 key lookup.
//
// This provides two symbols used by `node16.go`: `useSIMD` and `simdFindKey`.
// By default `useSIMD` is false and `simdFindKey` falls back to a simple
// linear search. To enable a real Go SIMD implementation, add a build-tag
// file (//go:build simd) that sets `useSIMD = true` and provides a
// high-performance `simdFindKey` implementation using your preferred SIMD
// package or assembly intrinsics. When building with a real SIMD backend
// you may need to set `GOEXPERIMENT=simd` or other environment flags per
// your toolchain and Go version.

// Toggle this to true in a //go:build simd file that provides a SIMD impl.
var useSIMD = false

// simdFindKey searches for `target` within the first `count` entries of
// `keys`. Returns the index (>=0) if found, otherwise -1. Keep this small
// and hot — it's intentionally in pure Go as a safe baseline.
func simdFindKey(keys *[16]byte, target byte, count uint8) int {
	// simple fast linear search; the loop is small and will be inlined.
	for i := 0; i < int(count); i++ {
		if keys[i] == target {
			return i
		}
	}
	return -1
}
