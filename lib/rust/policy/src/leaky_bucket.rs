//! Atomic bit-packed leaky bucket, dual to the token bucket.
use dashmap::DashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

const SCALE: u64 = 1000;
const FIELD_MAX: u64 = u32::MAX as u64;

fn pack(ts: u32, headroom: u32) -> u64 {
    ((ts as u64) << 32) | headroom as u64
}
fn unpack(v: u64) -> (u32, u32) {
    ((v >> 32) as u32, v as u32)
}
fn now_secs() -> u32 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs() as u32
}

pub struct LeakyBucket {
    state: AtomicU64,
}

impl LeakyBucket {
    pub fn new(capacity: u64) -> Self {
        let headroom = capacity.saturating_mul(SCALE).min(FIELD_MAX) as u32;
        Self {
            state: AtomicU64::new(pack(now_secs(), headroom)),
        }
    }

    pub fn allow(&self, capacity: u64, leak_rate: u64) -> bool {
        let now = now_secs();
        let full = capacity.saturating_mul(SCALE).min(FIELD_MAX);
        loop {
            let old = self.state.load(Ordering::Acquire);
            let (ts, raw) = unpack(old);
            let elapsed = now.saturating_sub(ts) as u64;
            let leaked = elapsed.saturating_mul(leak_rate).saturating_mul(SCALE);
            let headroom = (raw as u64).saturating_add(leaked).min(full);
            let Some(next_headroom) = headroom.checked_sub(SCALE) else {
                return false;
            };
            let new = pack(now, next_headroom as u32);
            if self
                .state
                .compare_exchange_weak(old, new, Ordering::AcqRel, Ordering::Relaxed)
                .is_ok()
            {
                return true;
            }
        }
    }
}

pub struct LeakyBucketLimiter {
    capacity: u64,
    leak_rate: u64,
    buckets: DashMap<String, Arc<LeakyBucket>>,
}

impl LeakyBucketLimiter {
    pub fn new(capacity: u64, leak_rate: u64) -> Self {
        Self {
            capacity,
            leak_rate,
            buckets: DashMap::new(),
        }
    }

    pub fn allow(&self, key: &str) -> bool {
        let bucket = self
            .buckets
            .entry(key.to_owned())
            .or_insert_with(|| Arc::new(LeakyBucket::new(self.capacity)))
            .clone();
        bucket.allow(self.capacity, self.leak_rate)
    }
}
