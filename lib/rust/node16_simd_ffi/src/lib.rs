// lib.rs — node16_simd_ffi
//
// Thin C-ABI wrapper around the SIMD Node16 key-search logic.
//
// This crate's *only* public surface is `node16_simd_find`.  All heavy SIMD
// lifting lives in the shared `node16_simd` module (copied verbatim from the
// main radixip_rs crate), keeping this crate self-contained with no intra-
// workspace dependencies.
//
// Future: as more ART SIMD operations are identified (Node48 byte-count,
// Node256 population-count, etc.) they can be added here and the main crate
// can import this one instead of duplicating the logic.

// Re-use the exact SIMD implementation from the main crate source.
// We include the file as a private module so cbindgen only sees the extern "C"
// function defined in *this* file, keeping the generated header minimal.
#[path = "../../src/art/node16_simd.rs"]
mod node16_simd;

use node16_simd::simd_find_child;

/// Search for `target` in the first `count` byte-keys of a Node16 slot array.
///
/// # Parameters
/// - `keys`   – Pointer to a 16-element byte array (the Node16 key slot).
///              Must not be NULL.
/// - `target` – The byte key to locate.
/// - `count`  – Number of *valid* entries in `keys` (0–16).
///
/// # Returns
/// The index (0–15) of the first matching entry, or **-1** on a miss.
///
/// # Safety
/// `keys` must point to a valid 16-byte array for the duration of this call.
/// The function is pure (no side-effects, no allocations) and is safe to call
/// from any thread concurrently.
#[no_mangle]
pub unsafe extern "C" fn node16_simd_find(
    keys: *const [u8; 16],
    target: u8,
    count: u8,
) -> i8 {
    // SAFETY: caller guarantees `keys` is non-null and points to 16 valid bytes.
    let keys_ref = unsafe { &*keys };
    match simd_find_child(keys_ref, target, count) {
        Some(idx) => idx as i8,
        None => -1,
    }
}
