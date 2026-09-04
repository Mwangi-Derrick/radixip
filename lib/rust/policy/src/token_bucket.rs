//! Atomic bit-packed Token Bucket
//!
//! One `AtomicU64` per IP. Layout:
//!
//! ```text
//!  63          32 31            0
//!  ┌─────────────┬──────────────┐
//!  │  unix secs  │ tokens×1000  │
//!  │  (32 bits)  │  (32 bits)   │
//!  └─────────────┴──────────────┘
//! ```
//!
//! The fixed-point tokens field stores `actual_tokens × 1000`, giving 3
//! decimal places of precision (e.g., 1500 = 1.5 tokens).
//!
//! The `allow()` method uses a CAS retry loop. Under realistic HTTP load
//! contention per bucket is essentially zero — benchmarks show 0 retries.

use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

// Bit layout helpers

/// Pack (timestamp_secs, tokens_fixed_point) into a single u64.
#[inline(always)]
pub fn pack(ts: u32, tokens_fp: u32) -> u64 {
    ((ts as u64) << 32) | (tokens_fp as u64)
}

/// Unpack a u64 into (timestamp_secs, tokens_fixed_point).
#[inline(always)]
pub fn unpack(v: u64) -> (u32, u32) {
    ((v >> 32) as u32, v as u32)
}

/// Current Unix timestamp as a u32 (seconds). Wraps in year 2106, fine for v1.
#[inline]
pub fn now_secs() -> u32 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs() as u32
}

// TokenBucket

/// A single IP's token bucket state stored in one `AtomicU64`.
///
/// This struct is held inside the `moka` cache — one per client IP.
pub struct TokenBucket {
    state: AtomicU64,
}

impl TokenBucket {
    /// Create a full bucket initialised with `capacity` tokens.
    pub fn new(capacity: u64) -> Self {
        let tokens_fp = (capacity * 1000).min(u32::MAX as u64) as u32;
        Self {
            state: AtomicU64::new(pack(now_secs(), tokens_fp)),
        }
    }

    /// Attempt to consume one token.
    ///
    /// Returns `true` if allowed, `false` if the bucket is empty.
    ///
    /// # Arguments
    /// - `capacity`: max tokens (burst ceiling)
    /// - `tokens_per_sec`: refill rate
    pub fn allow(&self, capacity: u64, tokens_per_sec: u64) -> bool {
        let now = now_secs();
        let cap_fp = (capacity * 1000).min(u32::MAX as u64) as u32;

        loop {
            let old = self.state.load(Ordering::Acquire);
            let (ts, raw_fp) = unpack(old);

            // Refill: add tokens proportional to elapsed seconds.
            let elapsed = now.saturating_sub(ts);
            let refill_fp = (elapsed as u64)
                .saturating_mul(tokens_per_sec * 1000)
                .min(u32::MAX as u64) as u32;
            let tokens_fp = raw_fp.saturating_add(refill_fp).min(cap_fp);

            if tokens_fp < 1000 {
                // Less than 1 whole token — reject.
                return false;
            }

            let new = pack(now, tokens_fp - 1000);

            match self
                .state
                .compare_exchange(old, new, Ordering::AcqRel, Ordering::Relaxed)
            {
                Ok(_) => return true,
                Err(_) => {
                    // Another thread updated the bucket concurrently.
                    // Retry with fresh load — CAS retries are rare in practice.
                    std::hint::spin_loop();
                }
            }
        }
    }

    /// Return the current approximate token count (for observability).
    pub fn tokens_approx(&self) -> f64 {
        let (_, tokens_fp) = unpack(self.state.load(Ordering::Relaxed));
        tokens_fp as f64 / 1000.0
    }
}
