//go:build simd
// +build simd

package art

// This file enables the SIMD shim when built with `-tags=simd`.
// Replace the body of `simdFindKey` with a real SIMD implementation
// (arch-specific intrinsics or the simd/archsimd package) to get
// actual vectorized performance. Building without `-tags=simd` will
// use the fallback in simd.go.

var useSIMD = true

func simdFindKey(keys *[16]byte, target byte, count uint8) int {
	// Placeholder optimized path: this matches the fallback but exists
	// to be replaced by a real SIMD implementation behind the build tag.
	for i := 0; i < int(count); i++ {
		if keys[i] == target {
			return i
		}
	}
	return -1
}
