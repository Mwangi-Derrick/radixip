//! Atomic fixed-window counter with a packed `(window_id, count)` state word.
use dashmap::DashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

pub struct FixedWindowCounter {
    limit: u32,
    window_ms: u64,
    states: DashMap<String, Arc<AtomicU64>>,
}

impl FixedWindowCounter {
    pub fn new(limit: u32, window_ms: u64) -> Self {
        Self {
            limit,
            window_ms,
            states: DashMap::new(),
        }
    }

    pub fn allow(&self, key: &str) -> bool {
        if self.limit == 0 || self.window_ms == 0 {
            return false;
        }
        let now_ms = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis() as u64;
        let id = (now_ms / self.window_ms) as u32;
        let cell = self
            .states
            .entry(key.to_owned())
            .or_insert_with(|| Arc::new(AtomicU64::new(0)))
            .clone();
        loop {
            let old = cell.load(Ordering::Acquire);
            let old_id = (old >> 32) as u32;
            let count = if old_id == id { old as u32 } else { 0 };
            if count >= self.limit {
                return false;
            }
            let new = ((id as u64) << 32) | (count + 1) as u64;
            if cell
                .compare_exchange_weak(old, new, Ordering::AcqRel, Ordering::Relaxed)
                .is_ok()
            {
                return true;
            }
        }
    }
}
