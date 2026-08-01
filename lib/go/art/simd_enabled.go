//go:build simd
// +build simd

package art

import "bytes"

// This file enables the SIMD shim when built with `-tags=simd`.
// It provides a fast, pure-Go implementation using the runtime's
// optimized `bytes.IndexByte` (which typically maps to a fast memchr).
// Replace with platform-specific intrinsics later if needed.

var useSIMD = true

func simdFindKey(keys *[16]byte, target byte, count uint8) int {
	if count == 0 {
		return -1
	}
	// Create a slice header for the prefix
	b := keys[:count]
	idx := bytes.IndexByte(b, target)
	return idx
}
