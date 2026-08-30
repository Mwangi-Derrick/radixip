//! Token Bucket Limiter
//!
//! Wraps a `moka::sync::Cache` (bounded, TTL-based eviction) of per-IP
//! `TokenBucket`s.  Supports three bucket key modes:
//!
//! - `Ip`     — one bucket per exact client IP
//! - `Subnet` — one bucket per LPM-matched CIDR prefix
//! - `Both`   — subnet check AND exact IP check (strictest)

use crate::token_bucket::TokenBucket;
use ipnetwork::IpNetwork;
use moka::sync::Cache;
use radixip::RadixEngine;
use radixip_config::{BucketKeyMode, RateLimitConfig};
use std::net::IpAddr;
use std::sync::Arc;

// ---------------------------------------------------------------------------
// Bucket key
// ---------------------------------------------------------------------------

/// The key used to look up or create a rate-limit bucket.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
enum BucketKey {
    Exact(IpAddr),
    Subnet(IpNetwork),
}

fn ip_to_key(ip: IpAddr, mode: &BucketKeyMode, engine: Option<&dyn RadixEngine>) -> BucketKey {
    match mode {
        BucketKeyMode::Ip => BucketKey::Exact(ip),
        BucketKeyMode::Subnet { depth_v4, depth_v6 } => {
            // First try LPM lookup in the RadixIP engine.
            if let Some(eng) = engine {
                if eng.lookup(&ip).is_some() {
                    // We have a matched prefix — use it as the key.
                    // Derive the network from the IP at the configured depth.
                    let prefix = truncate_to_prefix(ip, *depth_v4, *depth_v6);
                    return BucketKey::Subnet(prefix);
                }
            }
            // No match — fall back to exact IP.
            BucketKey::Exact(ip)
        }
        BucketKeyMode::Both { .. } => BucketKey::Exact(ip), // handled specially in `allow`
    }
}

/// Truncate an IP to the given prefix length to derive a network key.
fn truncate_to_prefix(ip: IpAddr, depth_v4: u8, depth_v6: u8) -> IpNetwork {
    match ip {
        IpAddr::V4(v4) => {
            let bits = u32::from(v4);
            let mask = if depth_v4 == 0 {
                0
            } else {
                !0u32 << (32 - depth_v4)
            };
            let net_addr = std::net::Ipv4Addr::from(bits & mask);
            IpNetwork::V4(ipnetwork::Ipv4Network::new(net_addr, depth_v4).unwrap())
        }
        IpAddr::V6(v6) => {
            let bits = u128::from(v6);
            let mask = if depth_v6 == 0 {
                0
            } else {
                !0u128 << (128 - depth_v6)
            };
            let net_addr = std::net::Ipv6Addr::from(bits & mask);
            IpNetwork::V6(ipnetwork::Ipv6Network::new(net_addr, depth_v6).unwrap())
        }
    }
}

// ---------------------------------------------------------------------------
// Limiter
// ---------------------------------------------------------------------------

/// Rate limiter backed by a bounded moka cache (TTL + LRU eviction).
pub struct TokenBucketLimiter {
    cache: Cache<BucketKey, Arc<TokenBucket>>,
    config: RateLimitConfig,
}

impl TokenBucketLimiter {
    /// Create from a validated [`RateLimitConfig`].
    pub fn new(config: RateLimitConfig) -> Self {
        let cache = Cache::builder()
            .max_capacity(config.max_buckets as u64)
            .time_to_idle(config.ttl())
            .build();
        Self { cache, config }
    }

    /// Test whether `ip` is allowed to proceed.
    ///
    /// - Returns `true`  → pass the request on.
    /// - Returns `false` → respond with 429.
    ///
    /// When `bucket_mode` is `Both`, the request must pass *both* the subnet
    /// bucket and the exact-IP bucket.
    pub fn allow(&self, ip: IpAddr, engine: Option<&dyn RadixEngine>) -> bool {
        match &self.config.bucket_mode {
            BucketKeyMode::Both { depth_v4, depth_v6 } => {
                let subnet_key = ip_to_key(
                    ip,
                    &BucketKeyMode::Subnet {
                        depth_v4: *depth_v4,
                        depth_v6: *depth_v6,
                    },
                    engine,
                );
                let exact_key = BucketKey::Exact(ip);
                self.consume(subnet_key) && self.consume(exact_key)
            }
            mode => self.consume(ip_to_key(ip, mode, engine)),
        }
    }

    fn consume(&self, key: BucketKey) -> bool {
        let bucket = self
            .cache
            .get_with(key, || Arc::new(TokenBucket::new(self.config.capacity)));

        // Just call allow() directly - no mutable access needed
        bucket.allow(self.config.capacity, self.config.refill_rate)
    }
    /// Return the number of tracked IPs/subnets currently in the store.
    pub fn tracked_count(&self) -> u64 {
        self.cache.entry_count()
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use radixip_config::RateLimitConfigBuilder;

    fn limiter(capacity: u64, refill_rate: u64) -> TokenBucketLimiter {
        let cfg = RateLimitConfig::builder()
            .capacity(capacity)
            .refill_rate(refill_rate)
            .build();
        TokenBucketLimiter::new(cfg)
    }

    #[test]
    fn allows_up_to_capacity() {
        let l = limiter(5, 0); // no refill so we exhaust cleanly
        let ip: IpAddr = "10.0.0.1".parse().unwrap();
        for _ in 0..5 {
            assert!(l.allow(ip, None));
        }
        // 6th request should be rejected
        assert!(!l.allow(ip, None));
    }

    #[test]
    fn different_ips_have_separate_buckets() {
        let l = limiter(1, 0);
        let ip1: IpAddr = "10.0.0.1".parse().unwrap();
        let ip2: IpAddr = "10.0.0.2".parse().unwrap();
        assert!(l.allow(ip1, None));
        assert!(l.allow(ip2, None)); // ip2 still has its own full bucket
        assert!(!l.allow(ip1, None));
    }
}
