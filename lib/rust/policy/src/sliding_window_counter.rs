//! Approximate sliding-window counter packed into one atomic word.
use dashmap::DashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

pub struct SlidingWindowCounter {
    limit: u16,
    window_ms: u64,
    states: DashMap<String, Arc<AtomicU64>>,
}

impl SlidingWindowCounter {
    pub fn new(limit: u16, window_ms: u64) -> Self {
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
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis() as u64;
        let id = (now / self.window_ms) as u32;
        let elapsed = now % self.window_ms;
        let cell = self
            .states
            .entry(key.to_owned())
            .or_insert_with(|| Arc::new(AtomicU64::new(0)))
            .clone();
        loop {
            let old = cell.load(Ordering::Acquire);
            let old_id = (old >> 32) as u32;
            let mut previous = (old >> 16) as u16;
            let mut current = old as u16;
            if old_id != id {
                if id.wrapping_sub(old_id) == 1 {
                    previous = current;
                } else {
                    previous = 0;
                }
                current = 0;
            }
            let weighted =
                (previous as u64).saturating_mul(self.window_ms - elapsed) / self.window_ms;
            if weighted.saturating_add(current as u64).saturating_add(1) > self.limit as u64 {
                return false;
            }
            current = current.saturating_add(1);
            let new = ((id as u64) << 32) | ((previous as u64) << 16) | current as u64;
            if cell
                .compare_exchange_weak(old, new, Ordering::AcqRel, Ordering::Relaxed)
                .is_ok()
            {
                return true;
            }
        }
    }
}
