//! optional Runtime configuration for RadixIP

use crate::traits::{EngineVariant, NodeVariant};

#[derive(Debug, Clone)]
pub struct RadixConfig {
    pub engine_variant: EngineVariant,
    pub node_variant: NodeVariant,
    pub cache_enabled: bool,
    pub cache_max_entries: usize,
    pub cache_ttl_seconds: Option<u64>,
    pub num_shards: Option<usize>,
    pub enable_stats: bool,
}

impl Default for RadixConfig {
    fn default() -> Self {
        Self {
            engine_variant: EngineVariant::Concurrent,
            node_variant: NodeVariant::Atomic,
            cache_enabled: true,
            cache_max_entries: 10000,
            cache_ttl_seconds: Some(3600),
            num_shards: None,
            enable_stats: true,
        }
    }
}

impl RadixConfig {
    pub fn new() -> Self {
        Self::default()
    }
    
    pub fn with_engine(mut self, variant: EngineVariant) -> Self {
        self.engine_variant = variant;
        self
    }
    
    pub fn with_node(mut self, variant: NodeVariant) -> Self {
        self.node_variant = variant;
        self
    }
    
    pub fn with_cache(mut self, enabled: bool, max_entries: usize) -> Self {
        self.cache_enabled = enabled;
        self.cache_max_entries = max_entries;
        self
    }
    
    pub fn high_performance() -> Self {
        Self {
            engine_variant: EngineVariant::LockFree,
            node_variant: NodeVariant::LockFree,
            cache_enabled: true,
            cache_max_entries: 100000,
            cache_ttl_seconds: None,
            num_shards: Some(32),
            enable_stats: false,
        }
    }
    
    pub fn memory_efficient() -> Self {
        Self {
            engine_variant: EngineVariant::Standard,
            node_variant: NodeVariant::Normal,
            cache_enabled: false,
            cache_max_entries: 0,
            cache_ttl_seconds: None,
            num_shards: None,
            enable_stats: false,
        }
    }
    
    pub fn balanced() -> Self {
        Self {
            engine_variant: EngineVariant::Concurrent,
            node_variant: NodeVariant::Atomic,
            cache_enabled: true,
            cache_max_entries: 10000,
            cache_ttl_seconds: Some(3600),
            num_shards: Some(16),
            enable_stats: true,
        }
    }
}