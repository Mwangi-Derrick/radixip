---
title: "Go SIMD via Rust FFI — Node16 Key Search"
description: >
  Why we bridge the Go ART Node16 to the Rust SIMD implementation
  via a CGo-loaded shared library rather than using Go's native SIMD.
status: adopted
date: 2026-08-11
authors:
  - Derrick Mwangi
---

# Go SIMD via Rust FFI — Node16 Key Search

## Context

The Adaptive Radix Tree (ART) Node16 stores up to 16 child keys in an
unsorted byte array.  Every lookup that lands on a Node16 needs to scan that
array for a match.  Doing this with a tight 16-iteration loop is fast, but a
single SIMD instruction can compare all 16 bytes simultaneously — roughly 3–5×
faster than the scalar loop on modern CPUs.

## Why Not Go's Native SIMD?

Go 1.26 ships `GOEXPERIMENT=simd`, which exposes `simd.Vector` and related
types.  We evaluated it and decided **not** to use it as the primary SIMD path
for these reasons:

| Concern | Detail |
|---|---|
| **Experimental status** | The API is gated behind `GOEXPERIMENT=simd` and explicitly marked "not production-ready" in Go 1.26 release notes. |
| **Architecture coverage** | At time of writing, Go SIMD is only usable on `x86_64`.  RadixIP targets `x86_64`, `aarch64` (Apple Silicon, AWS Graviton), and must degrade gracefully on others. |
| **Instruction availability** | Go's SIMD does not yet expose `_mm_movemask_epi8` or equivalent, the key instruction for the bitmask extraction pattern we use. |
| **Long-term churn risk** | An experimental API can change between minor Go releases, imposing maintenance burden on a library that promises stability. |

## The Solution: Rust SIMD via CGo

The Rust codebase already contains a **production-grade, runtime-dispatched
SIMD implementation** for Node16 at
[`lib/rust/src/art/node16_simd.rs`](../../lib/rust/src/art/node16_simd.rs):

| Path | Instruction set | Detection |
|---|---|---|
| `find_avx2` | AVX2 — 256-bit `ymm`, uses 128-bit lane | Runtime (`is_x86_feature_detected!`) |
| `find_sse4_1` | SSE4.1 — 128-bit `xmm` | Runtime (`is_x86_feature_detected!`) |
| `find_neon` | ARM NEON — `uint8x16_t` | Compile-time (mandatory on aarch64) |
| `find_scalar` | Pure Rust | Always (fallback) |

We expose this function through a minimal C ABI via the
[`node16_simd_ffi`](../../lib/rust/node16_simd_ffi/) crate, compile it to a
shared library, and load it from Go via CGo.

```
┌─────────────────────────────────────────────────────────────────┐
│ Go ART (lib/go/art/)                                            │
│  node16.go  ──findChild──▶  simdFindKey()                       │
│                              │ (simd_cgo.go, -tags simd_cgo)    │
│                              │                                   │
│                              ▼  CGo call                         │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ libnode16_simd_ffi.{so,dylib,dll}  (Rust cdylib)          │  │
│  │  node16_simd_find()                                        │  │
│  │    ├─ AVX2   (x86_64, runtime-detected)                    │  │
│  │    ├─ SSE4.1 (x86_64, runtime-detected)                    │  │
│  │    ├─ NEON   (aarch64, compile-time)                        │  │
│  │    └─ Scalar (fallback)                                     │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

## ABI Surface

The shared library exposes exactly one symbol:

```c
// node16_simd.h  (lib/vendor/include/)
int8_t node16_simd_find(const uint8_t (*keys)[16], uint8_t target, uint8_t count);
```

- **`keys`** — pointer to the Node16 key slot (16 bytes, caller-owned).
- **`target`** — the byte key being looked up.
- **`count`** — number of valid entries (0–16).
- **Returns** — index 0–15 on hit, -1 on miss.

The function is pure: no allocations, no side-effects, thread-safe.

## Build & Deployment

### First-time setup
```bash
# cbindgen is required; the Makefile installs it if missing:
make check-cbindgen
```

### Build the shared library
```bash
make build-simd-ffi
# This:
#   1. cargo build --release  (node16_simd_ffi crate)
#   2. Copies the .so/.dylib/.dll to lib/vendor/lib/
#   3. cbindgen regenerates lib/vendor/include/node16_simd.h
```

### Build / test Go with SIMD
```bash
make build-go-simd   # go build -tags simd_cgo ./...
make test-go-simd    # go test  -tags simd_cgo ./art/...
```

### Default (no SIMD)
```bash
go build ./...       # uses scalar path in simd.go — no CGo required
```

## Directory Layout

```
lib/
├── rust/
│   ├── src/art/node16_simd.rs      # Shared SIMD logic (also used by radixip_rs)
│   └── node16_simd_ffi/            # Standalone cdylib crate
│       ├── Cargo.toml
│       ├── build.rs                # cbindgen invocation
│       └── src/lib.rs              # extern "C" wrapper
├── go/art/
│   ├── simd.go                     # Scalar fallback (no build tags)
│   ├── simd_enabled.go             # bytes.IndexByte fast path (-tags simd)
│   └── simd_cgo.go                 # Rust FFI bridge   (-tags simd_cgo)  ← new
└── vendor/
    ├── include/node16_simd.h       # C header (generated, checked in as reference)
    └── lib/                        # compiled .so/.dylib/.dll (gitignored)
        └── libnode16_simd_ffi.so
```

## Memory & Safety

- The Rust function receives a raw pointer to the Go-owned key array.  Go's
  garbage collector will not move stack-allocated arrays during a CGo call
  (CGo pins them), so this is safe.
- The function is called with `unsafe.Pointer` in Go, which is the standard
  CGo pattern for passing fixed-size arrays.
- There are no Rust-side allocations; the shared object calls no `malloc`.

## Adding a New Architecture

1. Add a new `#[cfg(target_arch = "...")]` block in `node16_simd.rs` following
   the existing AVX2/NEON pattern.
2. Run `make build-simd-ffi` — cbindgen regenerates the header automatically.
3. No Go changes are required.

## Future Work

The `node16_simd_ffi` crate is intentionally structured as a **centralised SIMD
ops crate**.  When additional SIMD operations are identified (e.g., Node48
population-count, Node256 popcount-based rank), they can be added here and the
main `radixip_rs` crate can import this crate instead of duplicating the logic
in `node16_simd.rs`.

## References

- [`lib/rust/src/art/node16_simd.rs`](../../lib/rust/src/art/node16_simd.rs) — Rust SIMD implementation
- [`lib/rust/node16_simd_ffi/`](../../lib/rust/node16_simd_ffi/) — cdylib crate
- [`lib/go/art/simd_cgo.go`](../../lib/go/art/simd_cgo.go) — CGo bridge
- [`lib/vendor/include/node16_simd.h`](../../lib/vendor/include/node16_simd.h) — C header
- [Makefile](../../Makefile) — `build-simd-ffi`, `build-go-simd`, `test-go-simd`
- Go issue tracker: [golang/go#53171](https://github.com/golang/go/issues/53171) — native SIMD tracking issue
