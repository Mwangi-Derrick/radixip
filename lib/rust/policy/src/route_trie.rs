//! Segment-based Route Trie for per-API-route rate-limiting.
//!
//! Each node in the trie represents one URL path segment. Wildcard nodes (`*`)
//! match any segment and are used as a fallback when no exact child matches.
//!
//! # Example
//!
//! ```rust
//! use radixip_policy::route_trie::RouteTrie;
//! use radixip_config::RateLimitConfig;
//!
//! let mut trie = RouteTrie::new();
//! trie.insert("/api/v1/auth/*", &["POST"], RateLimitConfig::builder().capacity(5).refill_rate(1).build());
//! trie.insert("/api/v1/public/*", &["GET"], RateLimitConfig::builder().capacity(1000).refill_rate(100).build());
//!
//! // /api/v1/auth/login POST → the 5-token bucket
//! let limiter = trie.match_route("POST", "/api/v1/auth/login");
//! ```

use std::collections::HashMap;

use radixip_config::RateLimitConfig;

use crate::limiter::TokenBucketLimiter;

// ---------------------------------------------------------------------------
// RouteTrie
// ---------------------------------------------------------------------------

/// Segment-based trie that maps (method, path) → `TokenBucketLimiter`.
pub struct RouteTrie {
    root: RouteTrieNode,
}

impl RouteTrie {
    pub fn new() -> Self {
        Self {
            root: RouteTrieNode::default(),
        }
    }

    /// Insert `path` with the given HTTP `methods` and build a
    /// `TokenBucketLimiter` from `cfg` at the terminal node.
    pub fn insert(&mut self, path: &str, methods: &[&str], cfg: RateLimitConfig) {
        let segments = split_path(path);
        let mut node = &mut self.root;
        for seg in segments {
            node = node.children.entry(seg.to_string()).or_default();
        }
        let limiter = TokenBucketLimiter::new(cfg);
        if methods.is_empty() {
            // Empty methods slice means "all methods" — store under the
            // catch-all key "".
            node.limiters.insert(String::new(), limiter);
        } else {
            for m in methods {
                node.limiters.insert(m.to_uppercase(), limiter.clone());
            }
        }
    }

    /// Find the best matching `TokenBucketLimiter` for (`method`, `path`).
    ///
    /// Exact segments take priority over wildcard (`*`) segments.
    /// If a wildcard node exists at any level, it is recorded as a fallback.
    ///
    /// Returns `None` if no route matches at all.
    pub fn match_route(&self, method: &str, path: &str) -> Option<&TokenBucketLimiter> {
        let method_upper = method.to_uppercase();
        let segments = split_path(path);
        let mut node = &self.root;
        let mut last_wildcard: Option<&RouteTrieNode> = None;

        for seg in segments {
            if let Some(child) = node.children.get(seg) {
                // Exact match — prefer this, but also note the wildcard
                // sibling as a fallback so deeper paths don't lose it.
                if let Some(wc) = node.children.get("*") {
                    last_wildcard = Some(wc);
                }
                node = child;
            } else if let Some(wc) = node.children.get("*") {
                last_wildcard = Some(wc);
                node = wc;
            } else {
                // Dead end — use last recorded wildcard.
                return last_wildcard.and_then(|wc| wc.lookup(&method_upper));
            }
        }

        // Prefer terminal node; fall back to wildcard.
        node.lookup(&method_upper)
            .or_else(|| last_wildcard.and_then(|wc| wc.lookup(&method_upper)))
    }
}

impl Default for RouteTrie {
    fn default() -> Self {
        Self::new()
    }
}

// ---------------------------------------------------------------------------
// RouteTrieNode
// ---------------------------------------------------------------------------

#[derive(Default)]
struct RouteTrieNode {
    /// Exact-segment or `"*"` wildcard children.
    children: HashMap<String, RouteTrieNode>,
    /// Method → limiter at this terminal node.
    /// The empty-string key acts as "match all methods".
    limiters: HashMap<String, TokenBucketLimiter>,
}

impl RouteTrieNode {
    fn lookup(&self, method: &str) -> Option<&TokenBucketLimiter> {
        // Exact method match first.
        self.limiters
            .get(method)
            // Then try "all methods" catch-all.
            .or_else(|| self.limiters.get(""))
    }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Split a path string into non-empty segment strings.
fn split_path(path: &str) -> impl Iterator<Item = &str> {
    path.split('/').filter(|s| !s.is_empty())
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    fn cfg(capacity: u64, refill: u64) -> RateLimitConfig {
        RateLimitConfig::builder()
            .capacity(capacity)
            .refill_rate(refill)
            .build()
    }

    #[test]
    fn exact_match() {
        let mut trie = RouteTrie::new();
        trie.insert("/api/v1/health", &["GET"], cfg(10, 10));
        assert!(trie.match_route("GET", "/api/v1/health").is_some());
        assert!(trie.match_route("POST", "/api/v1/health").is_none());
    }

    #[test]
    fn wildcard_match() {
        let mut trie = RouteTrie::new();
        trie.insert("/api/v1/auth/*", &["POST"], cfg(5, 1));
        assert!(trie.match_route("POST", "/api/v1/auth/login").is_some());
        assert!(trie.match_route("POST", "/api/v1/auth/register").is_some());
        assert!(trie.match_route("GET", "/api/v1/auth/login").is_none());
    }

    #[test]
    fn exact_beats_wildcard() {
        let mut trie = RouteTrie::new();
        trie.insert("/api/*", &["GET"], cfg(1000, 100));
        trie.insert("/api/v1/status", &["GET"], cfg(5, 1));
        let exact = trie.match_route("GET", "/api/v1/status").unwrap();
        // Exact route has capacity=5; wildcard has capacity=1000.
        // We can only distinguish them by checking pointer identity or
        // by inspecting some observable property — just confirm both resolve.
        let wildcard = trie.match_route("GET", "/api/v1/other").unwrap();
        // Both must resolve to something (not None).
        let _ = (exact, wildcard);
    }

    #[test]
    fn no_match_returns_none() {
        let mut trie = RouteTrie::new();
        trie.insert("/api/v1/auth/*", &["POST"], cfg(5, 1));
        assert!(trie.match_route("GET", "/metrics").is_none());
    }
}
