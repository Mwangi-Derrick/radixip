use std::net::IpAddr;
#[cfg(feature = "redis")]
use std::sync::Arc;

use crate::config::RadixConfig;
use crate::engine::EngineWrapper;
use crate::traits::RadixEngine;
use crate::types::{EngineStats, Metadata};
use ipnetwork::IpNetwork;
#[cfg(feature = "redis")]
use radixip_cache::RedisClient;

pub struct HybridEngine {
    control_plane: EngineWrapper,
    data_plane: EngineWrapper,
    #[cfg(feature = "redis")]
    redis: Option<Arc<RedisClient>>,
    channel: String,
}

impl HybridEngine {
    pub async fn new(config: &RadixConfig) -> Result<Self, String> {
        let control_plane = EngineWrapper::new(
            config.engine_variant.clone(),
            config.node_variant.clone(),
            config.write_compressed,
            config.num_shards,
        );

        let data_plane = EngineWrapper::new(
            config.engine_variant.clone(),
            config.node_variant.clone(),
            config.read_compressed,
            config.num_shards,
        );

        #[cfg(feature = "redis")]
        let redis = if let Some(redis_config) = &config.redis {
            Some(Arc::new(
                RedisClient::new(redis_config.clone())
                    .await
                    .map_err(|e| e.to_string())?,
            ))
        } else {
            None
        };

        let engine = Self {
            control_plane,
            data_plane,
            #[cfg(feature = "redis")]
            redis,
            channel: config.redis_channel.clone(),
        };

        #[cfg(feature = "redis")]
        if let Some(r) = &engine.redis {
            // Boot-load the data plane from Redis
            if let Ok(entries) = r.hgetall_sync("radixip:entries") {
                for (cidr, meta_json) in entries {
                    if let Ok(ipnet) = cidr.parse::<IpNetwork>() {
                        if let Ok(meta) = serde_json::from_str::<Metadata>(&meta_json) {
                            let _ = engine.control_plane.insert(ipnet.clone(), meta.clone());
                            let _ = engine.data_plane.insert(ipnet, meta);
                        }
                    }
                }
            }
        }

        Ok(engine)
    }

    #[cfg(feature = "redis")]
    pub async fn start_sync(&self) -> Result<(), String> {
        if let Some(redis) = &self.redis {
            let redis_clone = redis.clone();
            let channel = self.channel.clone();
            let data_plane = Arc::new(self.data_plane.clone());

            tokio::spawn(async move {
                let handler = move |update: radixip_cache::redis::RedisCacheUpdate| {
                    let dp = data_plane.clone();
                    async move {
                        match update {
                            radixip_cache::redis::RedisCacheUpdate::Insert { prefix, metadata } => {
                                if let Ok(meta) = serde_json::from_value::<Metadata>(metadata) {
                                    if let Err(e) = dp.insert(prefix, meta) {
                                        eprintln!("Redis insert error: {e}");
                                    }
                                }
                            }
                            radixip_cache::redis::RedisCacheUpdate::Remove { prefix } => {
                                dp.remove(&prefix);
                            }
                            radixip_cache::redis::RedisCacheUpdate::Clear => {
                                dp.clear();
                            }
                        }
                    }
                };
                if let Err(e) = redis_clone
                    .subscribe_engine_updates(&channel, handler)
                    .await
                {
                    eprintln!("HybridEngine Redis sync stopped: {}", e);
                }
            });
        }
        Ok(())
    }
}

impl RadixEngine for HybridEngine {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        self.control_plane
            .insert(prefix.clone(), metadata.clone())?;

        #[cfg(feature = "redis")]
        if let Some(redis) = &self.redis {
            if let Ok(json_data) = serde_json::to_string(&metadata) {
                let _ = redis.hset_sync("radixip:entries", &prefix.to_string(), &json_data);
            }
            // publish_insert takes serde_json::Value to avoid a cyclic dep on the engine crate.
            if let Ok(meta_value) = serde_json::to_value(&metadata) {
                let redis = redis.clone();
                let channel = self.channel.clone();
                tokio::runtime::Handle::current().spawn(async move {
                    let _ = redis.publish_insert(&channel, prefix, meta_value).await;
                });
            }
        } else {
            let _ = self.data_plane.insert(prefix, metadata);
        }

        Ok(())
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        self.data_plane.lookup(ip)
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        let result = self.control_plane.remove(prefix);

        #[cfg(feature = "redis")]
        if let Some(redis) = &self.redis {
            let _ = redis.hdel_sync("radixip:entries", &prefix.to_string());
            let _ = redis.publish_remove(&self.channel, prefix.clone());
        } else {
            let _ = self.data_plane.remove(prefix);
        }

        result
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        self.data_plane.contains(prefix)
    }

    fn clear(&self) {
        self.control_plane.clear();
        #[cfg(feature = "redis")]
        if let Some(redis) = &self.redis {
            let _ = redis.publish_clear(&self.channel);
        } else {
            self.data_plane.clear();
        }
    }

    fn size(&self) -> usize {
        self.data_plane.size()
    }

    fn stats(&self) -> EngineStats {
        self.data_plane.stats()
    }
}
