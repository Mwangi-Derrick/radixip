use super::traits::*;
use crate::lpm::network_contains_ip;
use crate::types::{EngineStats, Metadata};
use ipnetwork::IpNetwork;
use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::{Arc, RwLock};

// Cache configuration between redis and engine
pub struct CacheConfig {
    pub max_entries: usize,
    pub ttl_seconds: Option<u64>,
}

pub struct RadixCache {
    cache: RwLock<HashMap<IpAddr, Option<Metadata>>>,
    config: CacheConfig,
    engine: Arc<dyn RadixEngine>,
    #[cfg(feature = "redis")]
    redis: Option<crate::redis::RedisClient>,
}

impl RadixCache {
    #[cfg(not(feature = "redis"))]
    pub fn new(config: CacheConfig, engine: Arc<dyn RadixEngine>) -> Self {
        Self {
            cache: RwLock::new(HashMap::new()),
            config,
            engine,
        }
    }

    #[cfg(feature = "redis")]
    pub fn new(
        config: CacheConfig,
        engine: Arc<dyn RadixEngine>,
        redis: Option<crate::redis::RedisClient>,
    ) -> Self {
        let cache = Self {
            cache: RwLock::new(HashMap::new()),
            config,
            engine: engine.clone(),
            redis,
        };

        // Boot-load prefixes from Redis into engine
        if let Some(r) = &cache.redis {
            if let Ok(entries) = r.hgetall_sync("radixip:entries") {
                for (cidr, meta_json) in entries {
                    if let Ok(network) = cidr.parse::<IpNetwork>() {
                        if let Ok(metadata) = serde_json::from_str::<Metadata>(&meta_json) {
                            let _ = engine.insert(network, metadata);
                        }
                    }
                }
            }
        }
        cache
    }

    pub fn lookup_with_cache(&self, ip: &IpAddr) -> Option<Metadata> {
        // Check cache first
        if let Some(entry) = self.cache.read().unwrap().get(ip) {
            return entry.clone();
        }

        // Cache miss - query engine
        let mut result = self.engine.lookup(ip);

        // If not found in engine, and redis is enabled, we could query Redis.
        // Wait, the engine has been boot-loaded with all prefixes!
        // So if it's not in the engine, it's not in Redis either.
        // However, if we want to query a shared lookup cache in Redis:
        #[cfg(feature = "redis")]
        if result.is_none() {
            if let Some(r) = &self.redis {
                let key = format!("radixip:lookup:{}", ip);
                if let Ok(Some(cached_meta)) = r.get_sync(&key) {
                    if let Ok(meta) = serde_json::from_str::<Metadata>(&cached_meta) {
                        result = Some(meta);
                    }
                }
            }
        }

        // Store in cache
        let mut cache = self.cache.write().unwrap();
        if cache.len() >= self.config.max_entries {
            // Simple eviction - remove oldest
            if let Some(key) = cache.keys().next().cloned() {
                cache.remove(&key);
            }
        }
        cache.insert(ip.clone(), result.clone());

        // Also store in Redis lookup cache
        #[cfg(feature = "redis")]
        if let (Some(r), Some(res)) = (&self.redis, &result) {
            if let Ok(json) = serde_json::to_string(res) {
                let key = format!("radixip:lookup:{}", ip);
                let _ = r.set_sync(&key, &json);
            }
        }

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
    #[cfg(not(feature = "redis"))]
    pub fn new(inner: Arc<dyn RadixEngine>, config: CacheConfig) -> Self {
        let cache = RadixCache::new(config, inner.clone());
        Self { inner, cache }
    }

    #[cfg(feature = "redis")]
    pub fn new(
        inner: Arc<dyn RadixEngine>,
        config: CacheConfig,
        redis: Option<crate::redis::RedisClient>,
    ) -> Self {
        let cache = RadixCache::new(config, inner.clone(), redis);
        Self { inner, cache }
    }
}

impl RadixEngine for CachedEngine {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        self.inner.insert(prefix, metadata.clone())?;

        // Persist to Redis
        #[cfg(feature = "redis")]
        if let Some(r) = &self.cache.redis {
            if let Ok(json) = serde_json::to_string(&metadata) {
                let _ = r.hset_sync("radixip:entries", &prefix.to_string(), &json);
            }
        }

        // Invalidate relevant cache entries
        self.cache.invalidate(&prefix);
        Ok(())
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        self.cache.lookup_with_cache(ip)
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        let result = self.inner.remove(prefix);

        // Remove from Redis
        #[cfg(feature = "redis")]
        if let Some(r) = &self.cache.redis {
            let _ = r.hdel_sync("radixip:entries", &prefix.to_string());
        }

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
