use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use std::net::IpAddr;
use ipnetwork::IpNetwork;
use super::traits::*;
use crate::lpm::network_contains_ip;
use crate::types::{EngineStats, Metadata};

// Cache configuration between redis and engine
pub struct CacheConfig {
    pub max_entries: usize,
    pub ttl_seconds: Option<u64>,
}

pub struct RadixCache {
    cache: RwLock<HashMap<IpAddr, Option<Metadata>>>,
    config: CacheConfig,
    engine: Arc<dyn RadixEngine>,
}

impl RadixCache {
    pub fn new(config: CacheConfig, engine: Arc<dyn RadixEngine>) -> Self {
        Self {
            cache: RwLock::new(HashMap::new()),
            config,
            engine,
        }
    }
    
    pub fn lookup_with_cache(&self, ip: &IpAddr) -> Option<Metadata> {
        // Check cache first
        if let Some(entry) = self.cache.read().unwrap().get(ip) {
            return entry.clone();
        }
        
        // Cache miss - query engine
        let result = self.engine.lookup(ip);
        
        // Store in cache
        let mut cache = self.cache.write().unwrap();
        if cache.len() >= self.config.max_entries {
            // Simple eviction - remove oldest
            if let Some(key) = cache.keys().next().cloned() {
                cache.remove(&key);
            }
        }
        cache.insert(ip.clone(), result.clone());
        
        result
    }
    
    pub fn invalidate(&self, prefix: &IpNetwork) {
        // Remove entries that match prefix
        let mut cache = self.cache.write().unwrap();
        cache.retain(|ip, _| !network_contains_ip(prefix, ip));
    }

    pub fn clear(&self) {
        self.cache.write().unwrap().clear();
    }
}

// Cache wrapper that implements RadixEngine
pub struct CachedEngine {
    inner: Arc<dyn RadixEngine>,
    cache: RadixCache,
}

impl CachedEngine {
    pub fn new(inner: Arc<dyn RadixEngine>, config: CacheConfig) -> Self {
        let cache = RadixCache::new(config, inner.clone());
        Self { inner, cache }
    }
}

impl RadixEngine for CachedEngine {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        self.inner.insert(prefix, metadata)?;
        // Invalidate relevant cache entries
        self.cache.invalidate(&prefix);
        Ok(())
    }
    
    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        self.cache.lookup_with_cache(ip)
    }
    
    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        let result = self.inner.remove(prefix);
        self.cache.invalidate(prefix);
        result
    }
    
    fn contains(&self, prefix: &IpNetwork) -> bool {
        self.inner.contains(prefix)
    }
    
    fn clear(&self) {
        self.inner.clear();
        self.cache.clear();
    }
    
    fn size(&self) -> usize {
        self.inner.size()
    }

    fn stats(&self) -> EngineStats {
        self.inner.stats()
    }
}
