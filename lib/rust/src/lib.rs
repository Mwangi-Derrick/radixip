//! RadixIP - High-performance IP subnet caching engine
//!
//! This library provides a lock-free binary radix tree for
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
pub mod redis;

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
pub use node::{AtomicNode, LockFreeNode, NodeWrapper, NormalNode, PaddedNode};
// Compressed (Patricia) node types
pub use node::NodeBuilder;
pub use node::{
    CompressedAtomicNode, CompressedLockFreeNode, CompressedNormalNode, CompressedPaddedNode,
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
    );

    if config.cache_enabled {
        let cache_config = CacheConfig {
            max_entries: config.cache_max_entries,
            ttl_seconds: config.cache_ttl_seconds,
        };
        #[cfg(feature = "redis")]
        {
            Box::new(CachedEngine::new(
                Arc::new(engine),
                cache_config,
                None, // Provide a way for advanced users to pass RedisClient
            ))
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
