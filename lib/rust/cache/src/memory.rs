use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::{Arc, RwLock};

#[derive(Clone, Debug)]
pub struct CacheConfig {
    pub max_entries: usize,
    pub ttl_seconds: Option<u64>,
}

#[derive(Clone, Debug)]
pub struct CacheEntry<T> {
    pub value: T,
    pub expires_at: Option<std::time::Instant>,
}

#[derive(Clone)]
pub struct RadixCache<T>
where
    T: Clone,
{
    cache: Arc<RwLock<HashMap<IpAddr, CacheEntry<T>>>>,
    pub config: CacheConfig,
}

impl<T> RadixCache<T>
where
    T: Clone,
{
    pub fn new(config: CacheConfig) -> Self {
        Self {
            cache: Arc::new(RwLock::new(HashMap::new())),
            config,
        }
    }

    pub fn get(&self, ip: &IpAddr) -> Option<T> {
        let should_remove = {
            let guard = self.cache.read().unwrap();
            let entry = guard.get(ip)?;
            if let Some(expires_at) = entry.expires_at {
                expires_at <= std::time::Instant::now()
            } else {
                false
            }
        };

        if should_remove {
            self.cache.write().unwrap().remove(ip);
            return None;
        }

        let guard = self.cache.read().unwrap();
        guard.get(ip).map(|entry| entry.value.clone())
    }

    pub fn insert(&self, ip: IpAddr, value: T) {
        let mut guard = self.cache.write().unwrap();
        if guard.len() >= self.config.max_entries {
            if let Some(oldest_key) = guard.keys().next().cloned() {
                guard.remove(&oldest_key);
            }
        }

        let expires_at = self
            .config
            .ttl_seconds
            .map(|ttl| std::time::Instant::now() + std::time::Duration::from_secs(ttl));

        guard.insert(ip, CacheEntry { value, expires_at });
    }

    pub fn invalidate_prefix<F>(&self, matches: F)
    where
        F: Fn(&IpAddr) -> bool,
    {
        let mut guard = self.cache.write().unwrap();
        guard.retain(|ip, _| !matches(ip));
    }

    pub fn clear(&self) {
        self.cache.write().unwrap().clear();
    }
}
