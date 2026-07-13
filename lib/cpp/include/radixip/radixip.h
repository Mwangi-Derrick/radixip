// radixip/lib/cpp/include/radixip/radixip.h
// C API Header

#ifndef RADIXIP_H
#define RADIXIP_H

#include <stddef.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

// Opaque handle to a RadixEngine
typedef struct RadixEngineHandle RadixEngineHandle;

// Create a new RadixEngine
RadixEngineHandle* radix_engine_new(void);

// Free a RadixEngine
void radix_engine_free(RadixEngineHandle* handle);

// Insert a subnet with metadata (JSON string)
int radix_engine_insert(
    RadixEngineHandle* handle,
    const char* subnet,
    const char* metadata_json
);

// Match an IP address (returns JSON string, must be freed)
char* radix_engine_match(
    const RadixEngineHandle* handle,
    const char* ip
);

// Check if an IP matches (boolean version)
bool radix_engine_contains(
    const RadixEngineHandle* handle,
    const char* ip
);

// Get the number of subnets
size_t radix_engine_size(const RadixEngineHandle* handle);

// Clear all subnets
void radix_engine_clear(RadixEngineHandle* handle);

// Free a string returned by radix_engine_match
void radix_engine_free_string(char* str);

// Get the library version
const char* radix_engine_version(void);

#ifdef __cplusplus
}
#endif

#endif // RADIXIP_H