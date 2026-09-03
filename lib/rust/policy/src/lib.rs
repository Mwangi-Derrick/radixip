//! RadixIP Policy crate
//!
//! Provides the per-IP Token Bucket rate limiter, IP extraction from HTTP
//! headers, and the `PolicyEngine` which composes them with a RadixIP
//! blocklist engine.
//!
//! # Quick start
//! ```no_run
//! use radixip_policy::PolicyEngine;
//! use radixip_config::RateLimitConfig;
//!
//! let cfg = RateLimitConfig::builder()
//!     .capacity(100)
//!     .refill_rate(10)
//!     .build();
//!
//! let engine = PolicyEngine::new(radix_engine, cfg);
//!
//! match engine.check(&ip, xff, x_real_ip, remote_addr) {
//!     PolicyDecision::Allow  => { /* pass */ }
//!     PolicyDecision::Block  => { /* 403 */ }
//!     PolicyDecision::Limit  => { /* 429 */ }
//! }
//! ```

pub mod auto_ban;
pub mod ip_extractor;
pub mod limiter;
pub mod route_trie;
pub mod token_bucket;
pub mod watcher;

pub use auto_ban::AutoBanTracker;
pub use ip_extractor::{extract_ip, ExtractError};
pub use limiter::TokenBucketLimiter;
pub use route_trie::RouteTrie;
pub use token_bucket::TokenBucket;
pub use watcher::{ConfigWatcher, PolicyState};

use radixip::RadixEngine;
use radixip_config::{MiddlewareConfig, RateLimitConfig};
use std::net::SocketAddr;
use std::sync::Arc;

// ---------------------------------------------------------------------------
// Policy decision
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PolicyDecision {
    /// Request is allowed — pass to next handler.
    Allow,
    /// IP is in the blocklist or was auto-banned — respond 403.
    Block,
    /// IP exceeded its token bucket — respond 429.
    Limit,
    /// IP was auto-banned due to repeated violations — respond 403.
    AutoBanned,
    /// Could not determine client IP — respond 400.
    BadRequest(String),
}

// ---------------------------------------------------------------------------
// PolicyEngine
// ---------------------------------------------------------------------------

/// Combines the RadixIP blocklist engine and the token-bucket rate limiter
/// into a single `check()` call suitable for any HTTP middleware.
pub struct PolicyEngine {
    engine: Arc<Box<dyn RadixEngine>>,
    limiter: TokenBucketLimiter,
    middleware_cfg: MiddlewareConfig,
    rate_limit_enabled: bool,
    blocklist_enabled: bool,
    auto_ban: Option<AutoBanTracker>,
}

impl PolicyEngine {
    pub fn new(
        engine: Arc<Box<dyn RadixEngine>>,
        middleware_cfg: MiddlewareConfig,
        rate_limit_cfg: RateLimitConfig,
        blocklist_enabled: bool,
    ) -> Self {
        let rate_limit_enabled = rate_limit_cfg.enabled;
        Self {
            engine,
            limiter: TokenBucketLimiter::new(rate_limit_cfg),
            middleware_cfg,
            rate_limit_enabled,
            blocklist_enabled,
            auto_ban: None,
        }
    }

    /// Attach an `AutoBanTracker` to this engine. When a rate-limit violation
    /// occurs, the tracker is notified and may inject a temporary ban.
    pub fn with_auto_ban(mut self, tracker: AutoBanTracker) -> Self {
        self.auto_ban = Some(tracker);
        self
    }

    /// Evaluate a request — returns the [`PolicyDecision`] the middleware
    /// should act on.
    pub fn check(
        &self,
        xff: Option<&str>,
        x_real_ip: Option<&str>,
        remote_addr: Option<&SocketAddr>,
    ) -> PolicyDecision {
        // 1. Extract IP.
        let ip = match extract_ip(
            xff,
            x_real_ip,
            remote_addr,
            &self.middleware_cfg.trusted_proxies,
        ) {
            Ok(ip) => ip,
            Err(e) => return PolicyDecision::BadRequest(e.to_string()),
        };

        // 2. Blocklist check (LPM in ~60ns).
        if self.blocklist_enabled && self.engine.lookup(&ip).is_some() {
            return PolicyDecision::Block;
        }

        // 3. Rate limit check (<200ns).
        if self.rate_limit_enabled && !self.limiter.allow(ip, Some(self.engine.as_ref().as_ref())) {
            // 3a. Notify auto-ban tracker on every violation.
            if let Some(ref tracker) = self.auto_ban {
                if tracker.record_violation(ip) {
                    return PolicyDecision::AutoBanned;
                }
            }
            return PolicyDecision::Limit;
        }

        PolicyDecision::Allow
    }

    pub fn limiter(&self) -> &TokenBucketLimiter {
        &self.limiter
    }

    pub fn engine(&self) -> &Arc<Box<dyn RadixEngine>> {
        &self.engine
    }
}
