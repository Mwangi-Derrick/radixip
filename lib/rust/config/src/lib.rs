//! RadixIP Configuration
//!
//! Provides typed configuration structs for the engine, middleware, blocklist,
//! and rate limiter. Supports loading from a YAML file or constructing
//! programmatically via the builder pattern.
//!
//! # Example (from file)
//! ```no_run
//! let cfg = RadixIpConfig::from_file("radixip.yaml").unwrap();
//! ```
//!
//! # Example (programmatic)
//! ```
//! let cfg = RateLimitConfig::builder()
//!     .capacity(100)
//!     .refill_rate(10)
//!     .bucket_mode(BucketKeyMode::Subnet { depth_v4: 24, depth_v6: 48 })
//!     .build();
//! ```

use serde::{Deserialize, Serialize};
use std::net::IpAddr;
use std::path::Path;
use std::time::Duration;
use thiserror::Error;

// ---------------------------------------------------------------------------
// Error
// ---------------------------------------------------------------------------

#[derive(Debug, Error)]
pub enum ConfigError {
    #[error("IO error reading config: {0}")]
    Io(#[from] std::io::Error),
    #[error("YAML parse error: {0}")]
    Yaml(#[from] serde_yaml::Error),
    #[error("Validation error: {0}")]
    Validation(String),
}

// ---------------------------------------------------------------------------
// Top-level
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct RadixIpConfig {
    pub radixip: RootConfig,
}

impl RadixIpConfig {
    /// Load from a YAML file path.
    pub fn from_file(path: impl AsRef<Path>) -> Result<Self, ConfigError> {
        let raw = std::fs::read_to_string(path)?;
        let cfg: Self = serde_yaml::from_str(&raw)?;
        cfg.validate()?;
        Ok(cfg)
    }

    /// Use a pre-parsed config directly (library consumers).
    pub fn from_config(config: RootConfig) -> Result<Self, ConfigError> {
        let this = Self { radixip: config };
        this.validate()?;
        Ok(this)
    }

    fn validate(&self) -> Result<(), ConfigError> {
        let rl = &self.radixip.rate_limit;
        if rl.enabled {
            if rl.capacity == 0 {
                return Err(ConfigError::Validation(
                    "rate_limit.capacity must be > 0".into(),
                ));
            }
            if rl.refill_rate == 0 {
                return Err(ConfigError::Validation(
                    "rate_limit.refill_rate must be > 0".into(),
                ));
            }
        }
        Ok(())
    }
}

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct RootConfig {
    #[serde(default)]
    pub engine: EngineConfig,
    #[serde(default)]
    pub middleware: MiddlewareConfig,
    #[serde(default)]
    pub blocklist: BlocklistConfig,
    #[serde(default)]
    pub rate_limit: RateLimitConfig,
    #[serde(default)]
    pub metrics: MetricsConfig,
}

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(default)]
pub struct EngineConfig {
    pub variant: String,      // "concurrent" | "standard" | "lock_free" | "adaptive"
    pub node_variant: String, // "atomic" | "normal" | "padded" | "lock_free"
    pub num_shards: Option<usize>,
    pub cache: CacheConfig,
}

impl Default for EngineConfig {
    fn default() -> Self {
        Self {
            variant: "concurrent".into(),
            node_variant: "atomic".into(),
            num_shards: Some(16),
            cache: CacheConfig::default(),
        }
    }
}

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(default)]
pub struct CacheConfig {
    pub enabled: bool,
    pub max_entries: usize,
    pub ttl_seconds: Option<u64>,
}

impl Default for CacheConfig {
    fn default() -> Self {
        Self {
            enabled: true,
            max_entries: 10_000,
            ttl_seconds: Some(3600),
        }
    }
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(default)]
pub struct MiddlewareConfig {
    /// Which header to extract the real client IP from.
    pub ip_source: IpSource,
    /// CIDRs of trusted reverse proxies (these are stripped from XFF chains).
    pub trusted_proxies: Vec<ipnetwork::IpNetwork>,
    pub responses: ResponseConfig,
}

impl Default for MiddlewareConfig {
    fn default() -> Self {
        Self {
            ip_source: IpSource::XForwardedFor,
            trusted_proxies: vec![],
            responses: ResponseConfig::default(),
        }
    }
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "kebab-case")]
pub enum IpSource {
    XForwardedFor,
    XRealIp,
    RemoteAddr,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(default)]
pub struct ResponseConfig {
    pub blocked: u16,      // default 403
    pub rate_limited: u16, // default 429
}

impl Default for ResponseConfig {
    fn default() -> Self {
        Self {
            blocked: 403,
            rate_limited: 429,
        }
    }
}

// ---------------------------------------------------------------------------
// Blocklist
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize, Serialize, Default)]
#[serde(default)]
pub struct BlocklistConfig {
    pub enabled: bool,
    pub sources: Vec<BlocklistSource>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum BlocklistSource {
    Inline { subnets: Vec<ipnetwork::IpNetwork> },
    File { path: String },
}

// ---------------------------------------------------------------------------
// Rate Limiting
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(default)]
pub struct RateLimitConfig {
    pub enabled: bool,
    /// Currently only "token_bucket".
    pub algorithm: RateLimitAlgorithm,
    /// How to key buckets: exact IP, subnet prefix, or both.
    pub bucket_mode: BucketKeyMode,
    /// Maximum burst capacity (tokens).
    pub capacity: u64,
    /// Token refill rate in tokens per second.
    pub refill_rate: u64,
    /// Maximum simultaneous tracked IPs. Default 1_000_000.
    pub max_buckets: usize,
    /// Evict idle IPs after this many seconds. Default 60.
    pub ttl_seconds: u64,
}

impl Default for RateLimitConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            algorithm: RateLimitAlgorithm::TokenBucket,
            bucket_mode: BucketKeyMode::Ip,
            capacity: 100,
            refill_rate: 10,
            max_buckets: 1_000_000,
            ttl_seconds: 60,
        }
    }
}

impl RateLimitConfig {
    pub fn builder() -> RateLimitConfigBuilder {
        RateLimitConfigBuilder::default()
    }

    pub fn ttl(&self) -> Duration {
        Duration::from_secs(self.ttl_seconds)
    }
}

#[derive(Debug, Clone, Deserialize, Serialize, Default)]
#[serde(rename_all = "snake_case")]
pub enum RateLimitAlgorithm {
    #[default]
    TokenBucket,
}

/// How to choose the key for a rate-limit bucket.
#[derive(Debug, Clone, Deserialize, Serialize, Default)]
#[serde(rename_all = "snake_case", tag = "mode")]
pub enum BucketKeyMode {
    /// One bucket per exact client IP (default).
    #[default]
    Ip,
    /// One bucket per longest-prefix-matched CIDR.
    /// Falls back to exact IP if no prefix matches.
    Subnet {
        depth_v4: u8, // typically 24 (class C)
        depth_v6: u8, // typically 48
    },
    /// Two checks: subnet bucket AND exact IP bucket (strictest).
    Both { depth_v4: u8, depth_v6: u8 },
}

// ---------------------------------------------------------------------------
// Builder for RateLimitConfig
// ---------------------------------------------------------------------------

#[derive(Debug, Default)]
pub struct RateLimitConfigBuilder {
    capacity: Option<u64>,
    refill_rate: Option<u64>,
    bucket_mode: Option<BucketKeyMode>,
    max_buckets: Option<usize>,
    ttl_seconds: Option<u64>,
}

impl RateLimitConfigBuilder {
    pub fn capacity(mut self, v: u64) -> Self {
        self.capacity = Some(v);
        self
    }
    pub fn refill_rate(mut self, v: u64) -> Self {
        self.refill_rate = Some(v);
        self
    }
    pub fn bucket_mode(mut self, m: BucketKeyMode) -> Self {
        self.bucket_mode = Some(m);
        self
    }
    pub fn max_buckets(mut self, v: usize) -> Self {
        self.max_buckets = Some(v);
        self
    }
    pub fn ttl_seconds(mut self, v: u64) -> Self {
        self.ttl_seconds = Some(v);
        self
    }

    pub fn build(self) -> RateLimitConfig {
        RateLimitConfig {
            enabled: true,
            algorithm: RateLimitAlgorithm::TokenBucket,
            bucket_mode: self.bucket_mode.unwrap_or_default(),
            capacity: self.capacity.unwrap_or(100),
            refill_rate: self.refill_rate.unwrap_or(10),
            max_buckets: self.max_buckets.unwrap_or(1_000_000),
            ttl_seconds: self.ttl_seconds.unwrap_or(60),
        }
    }
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(default)]
pub struct MetricsConfig {
    pub enabled: bool,
    pub prometheus_path: String,
}

impl Default for MetricsConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            prometheus_path: "/metrics".into(),
        }
    }
}
