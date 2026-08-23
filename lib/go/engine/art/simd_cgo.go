//go:build simd_cgo
// +build simd_cgo

// simd_cgo.go — CGo bridge to the Rust node16_simd_ffi shared library.
//
// Build with:
//   make build-simd-ffi   # compiles libnode16_simd_ffi.{so,dylib,dll}
//   go build -tags simd_cgo ./...
// or simply:
//   make build-go-simd
//
// The shared library must be present in lib/vendor/lib/ before the Go
// package can be compiled.  The Makefile handles this automatically.
//
// Architecture coverage (handled transparently by the Rust crate):
//   x86_64  — AVX2 → SSE4.1 → scalar  (runtime dispatch)
//   aarch64 — NEON mandatory baseline   (compile-time)
//   other   — scalar fallback

package art

/*
#cgo CFLAGS:  -I${SRCDIR}/../../../lib/vendor/include
#cgo LDFLAGS: -L${SRCDIR}/../../../lib/vendor/lib -lnode16_simd_ffi -Wl,-rpath,${SRCDIR}/../../../lib/vendor/lib
#include "node16_simd.h"
*/
import "C"
import "unsafe"

// useSIMD is true whenever this file is compiled (i.e. the simd_cgo build tag
// is active and the shared library was found at link time).
var useSIMD = true

// simdFindKey searches for target in keys[0:count] using the Rust SIMD
// implementation.  Returns the matched index (≥0) or -1 on miss.
//
// This function replaces the pure-Go implementation in simd.go and
// simd_enabled.go when built with -tags simd_cgo.
func simdFindKey(keys *[16]byte, target byte, count uint8) int {
	if count == 0 {
		return -1
	}
	result := C.node16_simd_find(
		(*[16]C.uint8_t)(unsafe.Pointer(keys)), // pointer to the 16-byte array
		C.uint8_t(target),
		C.uint8_t(count),
	)
	return int(result)
}
