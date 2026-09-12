//! Per-IP violation tracking and automatic temporary ban injection.
//!
//! When an IP accumulates `threshold_violations` rate-limit violations within
//! a sliding `window_seconds` window, it is automatically inserted into the
//! RadixIP blocklist engine as a /32 (IPv4) or /128 (IPv6) host route with
//! value `"auto-banned"`.  A background thread sweeps expired bans and
//! removes them from the engine.
//!
//! ## Concurrency model
//!
//! Violations are tracked in a `DashMap<IpAddr, Mutex<Vec<Instant>>>`.
//! Each IP has its own per-entry lock, so concurrent requests from *different*
//! IPs never contend with each other. The ban table is a plain
//! `DashMap<IpAddr, Instant>` where reads (the hot path) only hold a short
//! shard read-lock for a few nanoseconds.
//!
//! This is safe to use from both async Tokio contexts and sync FFI threads
//! (Python/Node) because no blocking I/O is performed under any lock.

use std::net::IpAddr;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use dashmap::DashMap;
use ipnetwork::IpNetwork;
use radixip::RadixEngine;
use radixip_config::AutoBanConfig;

// AutoBanTracker

/// Tracks per-IP violations and injects auto-bans into a `RadixEngine`.
///
/// Cheap to clone — all state lives behind `Arc`.
#[derive(Clone)]
pub struct AutoBanTracker {
    /// Per-IP violation timestamps (each IP holds its own short-lived lock).
    violations: Arc<DashMap<IpAddr, Mutex<Vec<Instant>>>>,
    /// Per-IP ban expiry. Reads only need a shard read-lock (very fast).
    banned: Arc<DashMap<IpAddr, Instant>>,
    threshold: u64,
    window: Duration,
    ban_duration: Duration,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl AutoBanTracker {
    /// Create a new tracker and start the background expiry sweeper.
    pub fn new(cfg: &AutoBanConfig, engine: Arc<Box<dyn RadixEngine>>) -> Self {
        let violations: Arc<DashMap<IpAddr, Mutex<Vec<Instant>>>> =
            Arc::new(DashMap::new());
        let banned: Arc<DashMap<IpAddr, Instant>> = Arc::new(DashMap::new());

        let tracker = Self {
            violations: Arc::clone(&violations),
            banned: Arc::clone(&banned),
            threshold: cfg.threshold_violations,
            window: Duration::from_secs(cfg.window_seconds),
            ban_duration: Duration::from_secs(cfg.ban_duration_seconds),
            engine: Arc::clone(&engine),
        };

        // Spawn background sweeper on a plain OS thread — works in both Tokio
        // and non-Tokio environments (Python/Node FFI).
        {
            let banned_clone = Arc::clone(&banned);
            let engine_clone = Arc::clone(&engine);
            std::thread::spawn(move || {
                sweeper(banned_clone, engine_clone);
            });
        }

        tracker
    }

    /// Record a rate-limit violation for `ip`.
    ///
    /// Returns `true` if the IP was auto-banned as a result of this call.
    /// Only holds the per-IP entry lock for the duration of the prune+count
    /// operation — never a global lock.
    pub fn record_violation(&self, ip: IpAddr) -> bool {
        let now = Instant::now();
        let cutoff = now.checked_sub(self.window).unwrap_or(now);

        // Get-or-insert the per-IP bucket (DashMap shard lock only).
        let entry = self
            .violations
            .entry(ip)
            .or_insert_with(|| Mutex::new(Vec::new()));

        {
            let mut timestamps = entry.value().lock().unwrap();

            // Prune old entries outside the sliding window.
            timestamps.retain(|&t| t >= cutoff);
            timestamps.push(now);

            if (timestamps.len() as u64) < self.threshold {
                return false; // lock released here
            }

            // Threshold reached — reset counter so a continuous flood doesn't
            // keep re-triggering the ban on every subsequent violation.
            timestamps.clear();
        } // per-IP lock released here

        // Record the ban and insert into the engine (no locks held).
        let expiry = now + self.ban_duration;
        self.banned.insert(ip, expiry);
        drop(entry); // release DashMap entry ref before engine call
        self.insert_ban(ip);
        true
    }

    /// Returns `true` if `ip` is within an active auto-ban period.
    ///
    /// Fast path: only acquires a DashMap shard read-lock for nanoseconds.
    pub fn is_banned(&self, ip: IpAddr) -> bool {
        self.banned
            .get(&ip)
            .map(|expiry| Instant::now() < *expiry)
            .unwrap_or(false)
    }

    fn insert_ban(&self, ip: IpAddr) {
        let prefix = host_prefix(ip);
        let meta = radixip::types::Metadata::new("auto-banned")
            .with_attribute("reason", "exceeded_threshold");
        let _ = self.engine.insert(prefix, meta);
    }
}

// Background sweeper

/// Runs forever on a background OS thread. Every 30 s it removes expired
/// bans from both the in-memory map and the RadixIP engine.
fn sweeper(banned: Arc<DashMap<IpAddr, Instant>>, engine: Arc<Box<dyn RadixEngine>>) {
    loop {
        std::thread::sleep(Duration::from_secs(30));

        let now = Instant::now();

        // Collect expired IPs (brief iteration scan over DashMap shards).
        let expired: Vec<IpAddr> = banned
            .iter()
            .filter(|r| now >= *r.value())
            .map(|r| *r.key())
            .collect();

        // Remove from ban map and engine (no locks held during engine calls).
        for ip in &expired {
            banned.remove(ip);
        }
        for ip in expired {
            let prefix = host_prefix(ip);
            engine.remove(&prefix);
        }
    }
}

// Helpers

/// Build a /32 (IPv4) or /128 (IPv6) host network for engine insertion.
fn host_prefix(ip: IpAddr) -> IpNetwork {
    match ip {
        IpAddr::V4(v4) => {
            IpNetwork::V4(ipnetwork::Ipv4Network::new(v4, 32).expect("32-bit mask always valid"))
        }
        IpAddr::V6(v6) => {
            IpNetwork::V6(ipnetwork::Ipv6Network::new(v6, 128).expect("128-bit mask always valid"))
        }
    }
}

