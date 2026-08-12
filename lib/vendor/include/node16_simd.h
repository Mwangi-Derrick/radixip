#ifndef NODE16_SIMD_FFI_H
#define NODE16_SIMD_FFI_H

#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

/**
 * Search for `target` in the first `count` byte-keys of a Node16 slot array.
 *
 * # Parameters
 * - `keys`   – Pointer to a 16-element byte array (the Node16 key slot).
 *              Must not be NULL.
 * - `target` – The byte key to locate.
 * - `count`  – Number of *valid* entries in `keys` (0–16).
 *
 * # Returns
 * The index (0–15) of the first matching entry, or **-1** on a miss.
 *
 * # Safety
 * `keys` must point to a valid 16-byte array for the duration of this call.
 * The function is pure (no side-effects, no allocations) and is safe to call
 * from any thread concurrently.
 */
int8_t node16_simd_find(const uint8_t (*keys)[16], uint8_t target, uint8_t count);

#endif  /* NODE16_SIMD_FFI_H */
