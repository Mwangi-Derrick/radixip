// node16_simd.rs — SIMD-accelerated key search for ART Node16
//
// Priority order (fastest first):
//   1. AVX2   — x86_64 with 256-bit registers  (ymm)
//   2. SSE4.1 — x86_64 with 128-bit registers  (xmm)  ← most common baseline
//   3. NEON   — aarch64 (Apple Silicon, AWS Graviton, RPi4+)
//   4. Scalar — safe fallback for everything else
//
// All paths are branchless once the feature is detected at runtime (x86) or
// compile-time (NEON is always present on aarch64).

/// Find the index of `target` in the first `count` entries of `keys`.
/// Returns `Some(index)` on hit, `None` on miss.
#[inline]
pub fn simd_find_child(keys: &[u8; 16], target: u8, count: u8) -> Option<usize> {
    if count == 0 {
        return None;
    }

    // x86_64
    #[cfg(target_arch = "x86_64")]
    {
        if is_x86_feature_detected!("avx2") {
            // SAFETY: guarded by runtime feature detection.
            return unsafe { find_avx2(keys, target, count) };
        }
        if is_x86_feature_detected!("sse4.1") {
            return unsafe { find_sse4_1(keys, target, count) };
        }
    }

    // aarch64 (NEON always available)
    #[cfg(target_arch = "aarch64")]
    {
        // SAFETY: NEON is mandatory on aarch64; no runtime check needed.
        return unsafe { find_neon(keys, target, count) };
    }

    // scalar fallback
    #[allow(unreachable_code)]
    find_scalar(keys, target, count)
}

// ---------------------------------------------------------------------------
// x86_64 — AVX2 (256-bit, but we only have 16 keys so use 128-bit lane
// AVX2 is 256 bit but it is a "vertical silo", shares 128 bit with SSE, that is why we use 128 bit lane and not 256 bit lane
// ---------------------------------------------------------------------------
#[cfg(target_arch = "x86_64")]
#[target_feature(enable = "avx2")]
unsafe fn find_avx2(keys: &[u8; 16], target: u8, count: u8) -> Option<usize> {
    use std::arch::x86_64::*;

    // Load 16 keys into a 128-bit SSE register (AVX2 includes SSE).
    let keys_vec = _mm_loadu_si128(keys.as_ptr() as *const __m128i);
    let target_vec = _mm_set1_epi8(target as i8);
    // Compare all 16 bytes at once — result byte is 0xFF on match, 0x00 on mismatch.
    let cmp = _mm_cmpeq_epi8(keys_vec, target_vec);
    // movemask: bit i is set when byte i of cmp is 0xFF.
    let mask = _mm_movemask_epi8(cmp) as u32;
    // Mask off bytes beyond count to prevent false positives from uninitialised slots.
    let valid_mask = mask & ((1u32 << count) - 1);
    if valid_mask == 0 {
        None
    } else {
        Some(valid_mask.trailing_zeros() as usize)
    }
}

// ---------------------------------------------------------------------------
// x86_64 — SSE4.1
// ---------------------------------------------------------------------------
#[cfg(target_arch = "x86_64")]
#[target_feature(enable = "sse4.1")]
unsafe fn find_sse4_1(keys: &[u8; 16], target: u8, count: u8) -> Option<usize> {
    use std::arch::x86_64::*;

    let keys_vec = _mm_loadu_si128(keys.as_ptr() as *const __m128i);
    let target_vec = _mm_set1_epi8(target as i8);
    let cmp = _mm_cmpeq_epi8(keys_vec, target_vec);
    let mask = _mm_movemask_epi8(cmp) as u32;
    let valid_mask = mask & ((1u32 << count) - 1);
    if valid_mask == 0 {
        None
    } else {
        Some(valid_mask.trailing_zeros() as usize)
    }
}

// ---------------------------------------------------------------------------
// aarch64 — ARM NEON
// ---------------------------------------------------------------------------
#[cfg(target_arch = "aarch64")]
#[target_feature(enable = "neon")]
unsafe fn find_neon(keys: &[u8; 16], target: u8, count: u8) -> Option<usize> {
    use std::arch::aarch64::*;

    // Broadcast target byte to all 16 lanes of a uint8x16_t register.
    let target_vec = vdupq_n_u8(target);
    // Load 16 key bytes into a NEON vector.
    let keys_vec = vld1q_u8(keys.as_ptr());
    // Compare: result lane is 0xFF on match.
    let cmp = vceqq_u8(keys_vec, target_vec);

    // Extract a 64-bit bitmask using the standard NEON "shrn" trick:
    // shift right each byte by 4 to pack 4 bits per byte, then reinterpret
    // the two 64-bit halves to build a 64-bit lane mask.
    let bit_mask: uint8x8_t = vshrn_n_u16(vreinterpretq_u16_u8(cmp), 4);
    let as_u64: u64 = vget_lane_u64(vreinterpret_u64_u8(bit_mask), 0);

    // Each matched position contributes a nibble (4 bits), so bit position
    // of match i is i*4.
    let valid = as_u64 & ((1u64 << (count as u64 * 4)) - 1);
    if valid == 0 {
        None
    } else {
        Some((valid.trailing_zeros() / 4) as usize)
    }
}

// ---------------------------------------------------------------------------
// Scalar fallback
// ---------------------------------------------------------------------------
#[inline(always)]
fn find_scalar(keys: &[u8; 16], target: u8, count: u8) -> Option<usize> {
    for i in 0..count as usize {
        if keys[i] == target {
            return Some(i);
        }
    }
    None
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_find_first() {
        let mut keys = [0u8; 16];
        keys[0] = 42;
        assert_eq!(simd_find_child(&keys, 42, 1), Some(0));
    }

    #[test]
    fn test_find_last_in_count() {
        let mut keys = [0u8; 16];
        for i in 0..16usize {
            keys[i] = i as u8;
        }
        assert_eq!(simd_find_child(&keys, 15, 16), Some(15));
    }

    #[test]
    fn test_miss() {
        let keys = [0u8; 16];
        assert_eq!(simd_find_child(&keys, 99, 16), None);
    }

    #[test]
    fn test_out_of_count_ignored() {
        // key 7 is at index 7 but count=5, so it should NOT be found.
        let mut keys = [0u8; 16];
        keys[7] = 7;
        assert_eq!(simd_find_child(&keys, 7, 5), None);
    }

    #[test]
    fn test_all_same_finds_first() {
        let keys = [5u8; 16];
        assert_eq!(simd_find_child(&keys, 5, 16), Some(0));
    }
}
