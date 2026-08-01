# Runtime-dispatch and ART FFI design

Goal
- Provide a clear, minimal C ABI for a Rust-backed ART engine so the Go `EngineWrapper` can optionally dispatch to it later.
- Keep ownership, threading, and error semantics explicit to avoid integration spaghetti.

Design summary
- Engine handle: `typedef void* art_engine_t;` — opaque pointer to boxed engine instance.
- ABI functions (C calling convention):
  - `art_engine_t art_engine_create(uint8_t variant);` // 0 on failure -> NULL
  - `void art_engine_free(art_engine_t e);`
  - `int art_engine_insert_cidr(art_engine_t e, const uint8_t* addr, uint8_t addr_len, uint8_t prefix_len, void* value);` // 0 OK
  - `void* art_engine_match_ip(art_engine_t e, const uint8_t* addr, uint8_t addr_len);` // NULL = not found
  - `int art_engine_delete_cidr(art_engine_t e, const uint8_t* addr, uint8_t addr_len, uint8_t prefix_len);` // 1 deleted, 0 not found
  - `size_t art_engine_size(art_engine_t e);`

Ownership and lifetimes
- `value` is an opaque pointer stored as-is by the engine. The caller owns value memory and must free it if needed.
- If engine-owned metadata is desired later, extend the ABI with alloc/free callbacks (function pointers) or use serialized blobs.

Threading and safety
- All ABI functions MUST be safe to call concurrently from multiple threads. The Rust implementation should wrap engines with internal synchronization (Mutex, RWLock) or implement sharded engines.

Error model
- Use integer return codes for errors (0 success, non-zero error). Prefer minimal codes for now:
  - 0 = OK
  - 1 = invalid args
  - 2 = OOM / internal error

FFI mapping notes (Rust)
- Export with `#[no_mangle] pub extern "C" fn ...`.
- Use `Box<T>` for engine instances and cast to `*mut c_void` for the opaque handle.
- Convert IP addresses as raw bytes: IPv4 length 4, IPv6 length 16. `addr_len` distinguishes them.

FFI mapping notes (Go)
- Use cgo to declare C functions and convert `net/netip` values to byte slices passed to C.
- Wrap returned `void*` as `unsafe.Pointer` in Go. Do NOT dereference; pass through to engine APIs.
- Provide a Go wrapper type that implements existing `RadixEngine` interface and delegates to the cgo functions.

Build and packaging
- For local dev: build Rust as a staticlib/dynamic library (`cd lib/rust && cargo build --release --lib --target-dir target/ffi`), export symbols.
- In Go, use `// #cgo LDFLAGS: -L${SRCDIR}/../../rust/target/release -lradixip_ffi` and `import "C"` in `lib/go/engine_rust.go` (adjust paths per repo layout).

Migration strategy
- Start with a pure-Go runtime-dispatch option (already present via `EngineVariant`).
- Add Rust-backed engine behind a new `EngineVariant` (e.g., `EngineAdaptiveRust`) once the ABI and wrappers are in place.

Next steps (chronological)
1. Implement `lib/rust/src/ffi.rs` with minimal ABI glue and build as a static lib.
2. Add `lib/go/engine_rust.go` cgo wrapper and register `EngineVariant` to create the Rust engine.
3. Add tests/benchmarks comparing Go pure-SIMD vs Rust ART to validate performance tradeoffs.

Notes
- Keep this doc authoritative for the ABI — update it if functions or ownership rules change.
