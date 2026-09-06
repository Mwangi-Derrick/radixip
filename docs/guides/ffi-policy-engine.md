# FFI Policy and Configuration Engine

This document proposes the next FFI layer for RadixIP configuration and policy enforcement. The existing C ABI exposes radix-tree engine operations, while the Python and Node bindings expose the engine through PyO3 and N-API. None of those boundaries currently expose the shared YAML configuration, token bucket, route trie, or auto-ban policy.

## Recommendation

Keep configuration parsing, route matching, token buckets, and auto-ban state inside Rust. Expose one opaque, thread-safe policy handle to native callers.

The request hot path should cross the boundary with compact values:

- a packed IPv4 or IPv6 address, not a string
- a method/path identifier only when route policies are enabled
- a small decision enum and optional retry duration on return

Do not parse YAML, allocate JSON, or construct a new policy object per request. The handle owns the `ConfigWatcher`, limiter state, route trie, auto-ban tracker, and shared blocklist engine.

## Proposed C ABI

The first implementation can use a stable C header like this:

```c
#include <stdint.h>
#include <stddef.h>

typedef struct RadixPolicyHandle RadixPolicyHandle;

typedef enum RadixPolicyDecision {
    RADIX_POLICY_ALLOW = 0,
    RADIX_POLICY_BLOCK = 1,
    RADIX_POLICY_LIMIT = 2,
    RADIX_POLICY_BAD_REQUEST = 3
} RadixPolicyDecision;

typedef struct RadixIpValue {
    uint8_t family;       /* 4 or 6 */
    uint8_t bytes[16];    /* IPv4 uses the first 4 bytes */
} RadixIpValue;

typedef struct RadixPolicyResult {
    RadixPolicyDecision decision;
    uint32_t retry_after_seconds;
} RadixPolicyResult;

RadixPolicyHandle* radix_policy_new_from_yaml(
    const char* config_path,
    RadixEngineHandle* engine,
    int* error_code
);

int radix_policy_check(
    RadixPolicyHandle* policy,
    const RadixIpValue* client_ip,
    const char* method,
    const char* path,
    RadixPolicyResult* result
);

int radix_policy_reload(RadixPolicyHandle* policy);
void radix_policy_free(RadixPolicyHandle* policy);
```

`radix_policy_check` should return an ABI error only for invalid handles or malformed input. Policy outcomes belong in `RadixPolicyResult`, so callers do not need to parse error strings. A later version can add a request struct containing trusted-proxy information, but the first version should accept an already extracted client IP.

## Ownership and concurrency

- `RadixPolicyHandle` is opaque and must be released with `radix_policy_free`.
- The handle must be safe for concurrent calls from C/C++, Python workers, and Node worker threads.
- YAML reload should atomically replace configuration-derived state while preserving the shared blocklist engine.
- The policy handle must own exactly one auto-ban tracker. Framework adapters should not add a second tracker around it.
- Returned results contain no borrowed pointers, so they are safe to copy across the ABI.

## Python and Node strategy

Python and Node should initially use native Rust bindings rather than routing through the C ABI:

- PyO3 can expose `Policy` and `PolicyResult` classes directly.
- N-API can expose `Policy` and a small result object directly.
- Both bindings can share the Rust policy implementation without C string conversion.
- Python calls should release the GIL around blocking reload or long-running operations; the decision path itself should avoid holding the GIL if practical.
- Node calls should keep the synchronous decision path allocation-light and provide an explicit async reload method if filesystem work is needed.

C and C++ should use the C ABI. Their FFI cost is expected to be the smallest because the call can pass a fixed-size IP struct and receive a fixed-size result without object conversion.

## FFI tax evaluation

The meaningful comparison is end-to-end request overhead, not an isolated function call. Add benchmarks for:

| Case | Operation | Measurements |
|---|---|---|
| Rust baseline | policy check in Rust | ns/op, allocations, p50/p99 |
| C ABI | packed IP to policy handle | ns/op, allocations |
| C ABI legacy | string IP to engine | ns/op, allocations |
| PyO3 | Python string/object call | ns/op, allocations, GIL time |
| PyO3 packed | Python `bytes`/integer IP call | ns/op, allocations |
| N-API | JavaScript string/object call | ns/op, allocations |
| N-API packed | Buffer or typed-array IP call | ns/op, allocations |

Run each case with allow, rate-limited, blocklisted, and auto-banned decisions. Also measure batches of 1, 100, and 1,000 requests to show whether boundary cost is amortized.

The likely result is:

- C/C++ packed calls: closest to the Rust baseline.
- PyO3/N-API packed calls: small but measurable conversion/runtime overhead.
- String and dictionary/object APIs: materially higher overhead from parsing and allocation.
- For a real HTTP request, framework parsing and network work may dominate the FFI difference; benchmark at both the policy-call and middleware levels.

These are hypotheses until the benchmark is implemented. Do not publish a performance claim for Python or Node from the current engine-only benchmarks.

## Implementation sequence

1. Add a Rust `PolicyHandle` module with packed-IP decision tests.
2. Add the C header and C ABI implementation with null/error handling tests.
3. Add a C++ smoke test and a C benchmark using the fixed-size request/result structs.
4. Add PyO3 `Policy` and `PolicyResult` bindings without exposing Rust internals.
5. Add N-API `Policy` and result bindings using `Buffer` or a fixed numeric representation.
6. Add cross-language benchmarks and compare against the Rust baseline.
7. Build Node and Python framework middleware on top of their native policy handles.

The existing engine C ABI should remain backward compatible. Policy FFI should be additive and should not make callers depend on Rust layout, `Arc`, or serialized configuration internals.
