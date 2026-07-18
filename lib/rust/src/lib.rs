//! RadixIP - High-performance IP subnet caching engine
//!
//! This library provides a lock-free binary radix tree for
//! longest-prefix matching of IP addresses against CIDR blocks.

pub mod engine;
pub mod node;
pub mod lpm;
pub mod cache;
pub mod types;
pub mod errors;
pub mod traits;
pub mod atomic;
pub mod config;


#[cfg(feature = "redis")]
pub mod redis;

#[cfg(feature = "ffi")]
pub mod ffi;

#[cfg(feature = "pyo3")]
pub mod python;

#[cfg(feature = "node")]
pub mod nodejs;

pub use engine::{EngineWrapper, StandardEngine, ShardedEngine};
pub use node::{NodeWrapper, NormalNode, AtomicNode, PaddedNode};
pub use lpm::LPM;
pub use types::{Metadata, SubnetRule};
pub use errors::{RadixError, Result};

pub use traits::*;
pub use config::RadixConfig;
pub use cache::{CachedEngine, CacheConfig};

use std::sync::Arc;

/// Create a new RadixIP engine with the given configuration
pub fn new(config: RadixConfig) -> Box<dyn RadixEngine> {
    let engine = EngineWrapper::new(config.engine_variant, config.node_variant);
    
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
            Box::new(CachedEngine::new(
                Arc::new(engine),
                cache_config,
            ))
        }
    } else {
        Box::new(engine)
    }
}

/// Create a high-performance RadixIP engine
pub fn new_high_performance() -> Box<dyn RadixEngine> {
    new(RadixConfig::high_performance())
}

/// Create a memory-efficient RadixIP engine
pub fn new_memory_efficient() -> Box<dyn RadixEngine> {
    new(RadixConfig::memory_efficient())
}

/// Create a balanced RadixIP engine
pub fn new_balanced() -> Box<dyn RadixEngine> {
    new(RadixConfig::balanced())
}

/// Library version
pub const VERSION: &str = env!("CARGO_PKG_VERSION");
