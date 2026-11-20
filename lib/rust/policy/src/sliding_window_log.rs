//! Exact sliding-window log. Exact event times require per-key timestamp
//! storage, so updates serialize only for the affected key.
use dashmap::DashMap;
use std::collections::VecDeque;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

pub struct SlidingWindowLog {
    limit: usize,
    window: Duration,
    logs: DashMap<String, Arc<Mutex<VecDeque<Instant>>>>,
}

impl SlidingWindowLog {
    pub fn new(limit: usize, window: Duration) -> Self {
        Self {
            limit,
            window,
            logs: DashMap::new(),
        }
    }

    pub fn allow(&self, key: &str, now: Instant) -> bool {
        if self.limit == 0 || self.window.is_zero() {
            return false;
        }
        let log = self
            .logs
            .entry(key.to_owned())
            .or_insert_with(|| Arc::new(Mutex::new(VecDeque::new())))
            .clone();
        let mut entries = log.lock().unwrap_or_else(|e| e.into_inner());
        let cutoff = now.checked_sub(self.window).unwrap_or(now);
        while entries.front().is_some_and(|t| *t <= cutoff) {
            entries.pop_front();
        }
        if entries.len() >= self.limit {
            return false;
        }
        entries.push_back(now);
        true
    }
}
