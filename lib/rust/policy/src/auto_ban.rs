//! Per-IP violation tracking and automatic temporary ban injection.
//!
//! When an IP accumulates `threshold_violations` rate-limit violations within
//! a sliding `window_seconds` window, it is automatically inserted into the
//! RadixIP blocklist engine as a /32 (IPv4) or /128 (IPv6) host route with
//! value `"auto-banned"`.  A background Tokio task sweeps expired bans and
//! removes them from the engine.

use std::collections::HashMap;
use std::net::{IpAddr, Ipv4Addr};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use ipnetwork::IpNetwork;
use radixip::RadixEngine;
use radixip_config::AutoBanConfig;

// ---------------------------------------------------------------------------
// AutoBanTracker
// ---------------------------------------------------------------------------

/// Shared inner state — wrapped in `Arc<Mutex<>>` so it is cheaply cloneable
/// across the background sweeper task.
struct Inner {
    /// Per-IP violation timestamps (sliding window).
    violations: HashMap<IpAddr, Vec<Instant>>,
    /// Per-IP ban expiry times.
    banned: HashMap<IpAddr, Instant>,
    threshold: u64,
    window: Duration,
    ban_duration: Duration,
}

impl Inner {
    fn prune_violations(&mut self, ip: IpAddr) {
        let cutoff = Instant::now().checked_sub(self.window).unwrap_or(Instant::now());
        if let Some(v) = self.violations.get_mut(&ip) {
            v.retain(|&t| t >= cutoff);
        }
    }
}

/// Tracks per-IP violations and injects auto-bans into a `RadixEngine`.
///
/// Cheap to clone — the backing data is behind an `Arc<Mutex<>>`.
#[derive(Clone)]
pub struct AutoBanTracker {
    inner: Arc<Mutex<Inner>>,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl AutoBanTracker {
    /// Create a new tracker and start the background expiry sweeper.
    pub fn new(cfg: &AutoBanConfig, engine: Arc<Box<dyn RadixEngine>>) -> Self {
        let inner = Arc::new(Mutex::new(Inner {
            violations: HashMap::new(),
            banned: HashMap::new(),
            threshold: cfg.threshold_violations,
            window: Duration::from_secs(cfg.window_seconds),
            ban_duration: Duration::from_secs(cfg.ban_duration_seconds),
        }));

        let tracker = Self {
            inner: Arc::clone(&inner),
            engine: Arc::clone(&engine),
        };

        // Start background sweeper.
        {
            let inner_clone = Arc::clone(&inner);
            let engine_clone = Arc::clone(&engine);
            tokio::spawn(async move {
                sweeper(inner_clone, engine_clone).await;
            });
        }

        tracker
    }

    /// Record a rate-limit violation for `ip`. Returns `true` if the IP was
    /// auto-banned as a result of this violation.
    pub fn record_violation(&self, ip: IpAddr) -> bool {
        let mut inner = self.inner.lock().unwrap();
        inner.prune_violations(ip);

        let violations = inner.violations.entry(ip).or_default();
        violations.push(Instant::now());

        if violations.len() as u64 >= inner.threshold {
            let expiry = Instant::now() + inner.ban_duration;
            inner.banned.insert(ip, expiry);
            // Reset violation counter so a single continuous flood doesn't
            // keep re-logging the ban.
            inner.violations.remove(&ip);
            drop(inner); // release lock before engine call
            self.insert_ban(ip);
            return true;
        }
        false
    }

    /// Returns `true` if `ip` is within an active auto-ban period tracked
    /// locally. Note: the engine blocklist already catches auto-banned IPs on
    /// the LPM check path, so this is provided for observability / testing.
    pub fn is_banned(&self, ip: IpAddr) -> bool {
        let inner = self.inner.lock().unwrap();
        inner
            .banned
            .get(&ip)
            .map(|&expiry| Instant::now() < expiry)
            .unwrap_or(false)
    }

    fn insert_ban(&self, ip: IpAddr) {
        let prefix = host_prefix(ip);
        let meta = radixip::types::Metadata::new("auto-banned")
            .with_attribute("reason", "exceeded_threshold");
        let _ = self.engine.insert(prefix, meta);
    }
}

// ---------------------------------------------------------------------------
// Background sweeper
// ---------------------------------------------------------------------------

async fn sweeper(inner: Arc<Mutex<Inner>>, engine: Arc<Box<dyn RadixEngine>>) {
    let mut interval = tokio::time::interval(Duration::from_secs(30));
    loop {
        interval.tick().await;

        let expired: Vec<IpAddr> = {
            let mut inner = inner.lock().unwrap();
            let now = Instant::now();
            let expired: Vec<IpAddr> = inner
                .banned
                .iter()
                .filter(|(_, &expiry)| now >= expiry)
                .map(|(&ip, _)| ip)
                .collect();
            for ip in &expired {
                inner.banned.remove(ip);
            }
            expired
        };

        for ip in expired {
            let prefix = host_prefix(ip);
            engine.remove(&prefix);
        }
    }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Build a /32 (IPv4) or /128 (IPv6) host network for engine insertion.
fn host_prefix(ip: IpAddr) -> IpNetwork {
    match ip {
        IpAddr::V4(v4) => IpNetwork::V4(
            ipnetwork::Ipv4Network::new(v4, 32).expect("32-bit mask always valid"),
        ),
        IpAddr::V6(v6) => IpNetwork::V6(
            ipnetwork::Ipv6Network::new(v6, 128).expect("128-bit mask always valid"),
        ),
    }
}
