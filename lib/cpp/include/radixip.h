#pragma once
// radixip.h - C ABI for RadixIP
// Link against libradixip.so / radixip.dll / libradixip.dylib
//
// Build the shared library first:
//   cargo build --release -p radixip-rs

#include <stddef.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

// Opaque engine handle
typedef struct RadixEngineHandle RadixEngineHandle;

// Lifecycle

/// Create a balanced radix engine. Returns NULL on failure.
RadixEngineHandle* radix_engine_new(void);

/// Destroy the engine and free all memory.
void radix_engine_free(RadixEngineHandle* handle);

// Mutations

/// Insert a CIDR subnet with JSON metadata.
/// @param subnet   e.g. "192.168.1.0/24"
/// @param metadata JSON string, e.g. '{"action":"block","asn":"AS12345"}'
/// @return 0 on success, negative on error.
int radix_engine_insert(RadixEngineHandle* handle,
                        const char*        subnet,
                        const char*        metadata);

/// Remove a subnet from the engine.
/// @return 0 if removed, -1 if not found.
int radix_engine_remove(RadixEngineHandle* handle, const char* subnet);

/// Remove all entries.
void radix_engine_clear(RadixEngineHandle* handle);

// Queries

/// Longest-prefix match an IP address.
/// @param ip  e.g. "203.0.113.5"
/// @return    Heap-allocated JSON string on match, NULL on no-match.
///            Caller MUST free with radix_engine_free_string().
char* radix_engine_match(const RadixEngineHandle* handle, const char* ip);

/// Boolean check — returns true if any prefix covers the IP.
bool radix_engine_contains(const RadixEngineHandle* handle, const char* ip);

/// Return the number of stored prefixes.
size_t radix_engine_size(const RadixEngineHandle* handle);

// Strings returned by the library

/// Free a C-string that was returned by the library (e.g. from radix_engine_match).
void radix_engine_free_string(char* ptr);

// Metadata

/// Null-terminated semantic version string (e.g. "0.1.0").
const char* radix_engine_version(void);

#ifdef __cplusplus
} // extern "C"
#endif
