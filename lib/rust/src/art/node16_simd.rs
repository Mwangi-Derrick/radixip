// node16_simd.rs
use std::simd::prelude::*;

pub fn simd_find_child(keys: &[u8; 16], target: u8, count: u8) -> Option<usize> {
    // Ensure we have SIMD support
    #[cfg(target_arch = "x86_64")]
    {
        if is_x86_feature_detected!("avx2") {
            return unsafe { simd_find_child_avx2(keys, target) };
        }
    }
    
    // Fallback: Linear search
    for i in 0..count as usize {
        if keys[i] == target {
            return Some(i);
        }
    }
    None
}

#[cfg(target_arch = "x86_64")]
#[target_feature(enable = "avx2")]
unsafe fn simd_find_child_avx2(keys: &[u8; 16], target: u8) -> Option<usize> {
    let target_vec = Simd::<u8, 16>::splat(target);
    let keys_vec = Simd::<u8, 16>::from_array(*keys);
    
    let mask = keys_vec.simd_eq(target_vec);
    if mask.any() {
        let bitmask = mask.to_bitmask();
        Some(bitmask.trailing_zeros() as usize)
    } else {
        None
    }
}