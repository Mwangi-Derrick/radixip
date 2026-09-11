//! RadixIP - High-performance IP subnet caching engine
//!
//! This library provides three trees, a binary trie, a radix tree and Adaptive Radix Tree (ART),for
//! longest-prefix matching of IP addresses against CIDR blocks.

pub mod art;
pub mod atomic;
pub mod cache;
pub mod config;
pub mod engine;
pub mod engine_art;
pub mod errors;
pub mod hybrid;
pub mod lpm;
pub mod node;
pub mod traits;
pub mod tree;
pub mod types;

#[cfg(feature = "redis")]
pub use radixip_cache as redis;

#[cfg(feature = "ffi")]
pub mod ffi;

#[cfg(feature = "pyo3")]
pub mod python;

#[cfg(feature = "node")]
pub mod nodejs;

pub use engine::{EngineWrapper, ShardedEngine, StandardEngine};
pub use engine_art::ARTEngineAdapter;
pub use errors::{RadixError, Result};
pub use hybrid::HybridEngine;
pub use lpm::LPM;
// Uncompressed node types
pub use node::{AtomicTrieNode, LockFreeTrieNode, NodeWrapper, NormalTrieNode, PaddedTrieNode};
pub use node::NodeBuilder;
// Compressed (Patricia/Radix) node types
pub use node::{
    AtomicRadixNode, LockFreeRadixNode, NormalRadixNode, PaddedRadixNode,
};
pub use types::{Metadata, SubnetRule};

pub use cache::{CacheConfig, CachedEngine};
pub use config::RadixConfig;
pub use traits::*;

use std::sync::Arc;

/// Create a new RadixIP engine with the given configuration
pub async fn new(config: RadixConfig) -> Box<dyn RadixEngine> {
    if config.enable_split_plane {
        return Box::new(
            HybridEngine::new(&config)
                .await
                .expect("Failed to initialize HybridEngine"),
        );
    }

    let engine = EngineWrapper::new(
        config.engine_variant,
        config.node_variant,
        config.read_compressed,
        config.num_shards,
    );

    if config.cache_enabled {
        let cache_config = CacheConfig {
            max_entries: config.cache_max_entries,
            ttl_seconds: config.cache_ttl_seconds,
        };
        #[cfg(feature = "redis")]
        {
            let redis = if let Some(redis_config) = config.redis.clone() {
                Some(
                    radixip_cache::RedisClient::new(redis_config)
                        .await
                        .expect("Failed to initialize Redis cache client"),
                )
            } else {
                None
            };

            Box::new(CachedEngine::new(Arc::new(engine), cache_config, redis))
        }
        #[cfg(not(feature = "redis"))]
        {
            Box::new(CachedEngine::new(Arc::new(engine), cache_config))
        }
    } else {
        Box::new(engine)
    }
}

/// Create a high-performance RadixIP engine
pub async fn new_high_performance() -> Box<dyn RadixEngine> {
    new(RadixConfig::high_performance()).await
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn redis_config_builder_tracks_cache_and_channel() {
        let mut config = RadixConfig::new();
        config.cache_enabled = true;
        config.redis_channel = "radixip:test".to_string();
        config.redis = Some(radixip_cache::RedisConfig {
            url: "redis://127.0.0.1:6379".to_string(),
            pool_size: 4,
            connect_timeout: std::time::Duration::from_secs(2),
            max_retries: 1,
        });

        assert!(config.cache_enabled);
        assert_eq!(config.redis_channel, "radixip:test");
        assert!(config.redis.is_some());
    }
}

/// Create a memory-efficient RadixIP engine
pub async fn new_memory_efficient() -> Box<dyn RadixEngine> {
    new(RadixConfig::memory_efficient()).await
}

/// Create a balanced RadixIP engine
pub async fn new_balanced() -> Box<dyn RadixEngine> {
    new(RadixConfig::balanced()).await
}

/// Library version
pub const VERSION: &str = env!("CARGO_PKG_VERSION");
