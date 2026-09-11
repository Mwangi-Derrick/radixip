use futures_util::StreamExt;
use ipnetwork::IpNetwork;
use redis::{aio::ConnectionManager, AsyncCommands, Client, RedisError};
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::{broadcast, mpsc, Mutex};
use tokio::task::JoinHandle;
use tracing::{debug, error, info};

#[derive(Debug, thiserror::Error)]
pub enum RedisPubSubError {
    #[error("Redis error: {0}")]
    Redis(#[from] RedisError),
    #[error("Channel send error")]
    SendError,
    #[error("Channel receive error")]
    RecvError,
}

pub type Result<T> = std::result::Result<T, RedisPubSubError>;

#[derive(Debug, Clone)]
pub struct RedisConfig {
    pub url: String,
    pub pool_size: usize,
    pub connect_timeout: Duration,
    pub max_retries: usize,
}

impl Default for RedisConfig {
    fn default() -> Self {
        Self {
            url: "redis://127.0.0.1:6379".to_string(),
            pool_size: 10,
            connect_timeout: Duration::from_secs(5),
            max_retries: 3,
        }
    }
}

#[derive(Clone)]
pub struct RedisClient {
    inner: Arc<RedisClientInner>,
}

struct RedisClientInner {
    client: Client,
    connection_manager: Mutex<ConnectionManager>,
    config: RedisConfig,
    pubsub_sender: broadcast::Sender<PubSubMessage>,
    shutdown_tx: broadcast::Sender<()>,
}

#[derive(Debug, Clone)]
pub struct PubSubMessage {
    pub channel: String,
    pub payload: String,
    pub pattern: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "op", rename_all = "snake_case")]
pub enum RedisCacheUpdate {
    Insert {
        prefix: IpNetwork,
        metadata: serde_json::Value,
    },
    Remove {
        prefix: IpNetwork,
    },
    Clear,
}

impl RedisClient {
    pub async fn new(config: RedisConfig) -> Result<Self> {
        let client = Client::open(config.url.clone())?;
        let connection_manager = ConnectionManager::new(client.clone())
            .await
            .map_err(RedisPubSubError::Redis)?;
        let (pubsub_tx, _) = broadcast::channel(100);
        let (shutdown_tx, _) = broadcast::channel(1);

        let inner = RedisClientInner {
            client,
            connection_manager: Mutex::new(connection_manager),
            config,
            pubsub_sender: pubsub_tx,
            shutdown_tx,
        };

        Ok(Self {
            inner: Arc::new(inner),
        })
    }

    async fn get_connection(&self) -> Result<tokio::sync::MutexGuard<'_, ConnectionManager>> {
        Ok(self.inner.connection_manager.lock().await)
    }

    pub fn get_sync_connection(&self) -> Result<redis::Connection> {
        self.inner
            .client
            .get_connection()
            .map_err(RedisPubSubError::Redis)
    }

    pub async fn publish(&self, channel: &str, message: &str) -> Result<()> {
        let mut conn = self.get_connection().await?;
        let _: usize = conn.publish(channel, message).await?;
        debug!("Published to {}: {}", channel, message);
        Ok(())
    }

    pub async fn publish_json<T: serde::Serialize>(&self, channel: &str, data: &T) -> Result<()> {
        let json = serde_json::to_string(data).map_err(|e| {
            RedisPubSubError::Redis(RedisError::from((
                redis::ErrorKind::TypeError,
                "Serialization error",
                e.to_string(),
            )))
        })?;
        self.publish(channel, &json).await
    }

    pub async fn subscribe<F, Fut>(&self, channel: &str, mut callback: F) -> Result<JoinHandle<()>>
    where
        F: FnMut(PubSubMessage) -> Fut + Send + 'static,
        Fut: std::future::Future<Output = ()> + Send,
    {
        let client = self.inner.client.clone();
        let channel_name = channel.to_string();
        let shutdown_rx = self.inner.shutdown_tx.subscribe();

        let handle = tokio::spawn(async move {
            let mut shutdown = shutdown_rx;
            let mut pubsub = match client.get_async_pubsub().await {
                Ok(pubsub) => pubsub,
                Err(error) => {
                    error!("Failed to create Redis Pub/Sub connection: {}", error);
                    return;
                }
            };
            if let Err(error) = pubsub.subscribe(&channel_name).await {
                error!("Failed to subscribe to {}: {}", channel_name, error);
                return;
            }
            let mut stream = pubsub.on_message();

            loop {
                tokio::select! {
                    msg_result = stream.next() => {
                        match msg_result {
                            Some(msg) => {
                                let payload: String = msg.get_payload().unwrap_or_default();
                                let channel_name = msg.get_channel_name().to_string();
                                let pubsub_msg = PubSubMessage { channel: channel_name, payload, pattern: None };
                                callback(pubsub_msg).await;
                            }
                            None => {
                                debug!("PubSub stream ended for channel {}", channel_name);
                                break;
                            }
                        }
                    }
                    _ = shutdown.recv() => {
                        info!("Shutting down subscription for channel {}", channel_name);
                        break;
                    }
                }
            }
        });

        Ok(handle)
    }

    /// Synchronous Set
    pub fn set_sync(&self, key: &str, value: &str) -> Result<()> {
        let mut conn = self.get_sync_connection()?;
        let _: () = redis::cmd("SET").arg(key).arg(value).query(&mut conn)?;
        Ok(())
    }

    /// Synchronous Get
    pub fn get_sync(&self, key: &str) -> Result<Option<String>> {
        let mut conn = self.get_sync_connection()?;
        let result: Option<String> = redis::cmd("GET").arg(key).query(&mut conn)?;
        Ok(result)
    }

    /// Synchronous HGetAll for boot-loading prefixes
    /// Use this ONLY for initial boot-loading, NOT for incremental updates
    pub fn hgetall_sync(&self, key: &str) -> Result<std::collections::HashMap<String, String>> {
        let mut conn = self.get_sync_connection()?;
        let result: std::collections::HashMap<String, String> =
            redis::cmd("HGETALL").arg(key).query(&mut conn)?;
        Ok(result)
    }

    /// Use this ONLY for initial boot-loading, NOT for incremental updates
    pub fn hset_sync(&self, key: &str, field: &str, value: &str) -> Result<()> {
        let mut conn = self.get_sync_connection()?;
        let _: () = redis::cmd("HSET")
            .arg(key)
            .arg(field)
            .arg(value)
            .query(&mut conn)?;
        Ok(())
    }

    /// Synchronous HDel
    pub fn hdel_sync(&self, key: &str, field: &str) -> Result<()> {
        let mut conn = self.get_sync_connection()?;
        let _: () = redis::cmd("HDEL").arg(key).arg(field).query(&mut conn)?;
        Ok(())
    }

    /// Publish an insert update for a prefix with JSON-serialized metadata.
    ///
    /// The `metadata` must be a `serde_json::Value` so this crate does not
    /// depend on the engine crate (which would create a cyclic dependency).
    pub async fn publish_insert(
        &self,
        channel: &str,
        prefix: IpNetwork,
        metadata: serde_json::Value,
    ) -> Result<()> {
        self.publish_json(channel, &RedisCacheUpdate::Insert { prefix, metadata })
            .await
    }

    pub async fn publish_clear(&self, channel: &str) -> Result<()> {
        self.publish_json(channel, &RedisCacheUpdate::Clear).await
    }

    pub async fn publish_remove(&self, channel: &str, prefix: IpNetwork) -> Result<()> {
        self.publish_json(channel, &RedisCacheUpdate::Remove { prefix })
            .await
    }

    /// Subscribe and dispatch decoded `RedisCacheUpdate` events through a callback.
    ///
    /// Using a callback avoids importing engine traits here, preventing a
    /// cyclic dependency.
    pub async fn subscribe_engine_updates<F, Fut>(
        &self,
        channel: &str,
        on_update: F,
    ) -> Result<JoinHandle<()>>
    where
        F: Fn(RedisCacheUpdate) -> Fut + Send + Sync + 'static,
        Fut: std::future::Future<Output = ()> + Send + 'static,
    {
        self.subscribe(channel, move |message| {
            match serde_json::from_str::<RedisCacheUpdate>(&message.payload) {
                Ok(update) => on_update(update),
                Err(e) => {
                    error!("Failed to decode Redis cache update: {}", e);
                    on_update(RedisCacheUpdate::Clear)
                }
            }
        })
        .await
    }

    pub async fn subscribe_to_channel(
        &self,
        channel: &str,
    ) -> Result<(mpsc::Receiver<PubSubMessage>, JoinHandle<()>)> {
        let (tx, rx) = mpsc::channel(100);
        let tx_clone = tx.clone();

        let client = self.inner.client.clone();
        let channel_name = channel.to_string();
        let shutdown_rx = self.inner.shutdown_tx.subscribe();

        let handle = tokio::spawn(async move {
            let mut shutdown = shutdown_rx;
            let mut pubsub = match client.get_async_pubsub().await {
                Ok(pubsub) => pubsub,
                Err(error) => {
                    error!("Failed to create Redis Pub/Sub connection: {}", error);
                    return;
                }
            };
            if let Err(error) = pubsub.subscribe(&channel_name).await {
                error!("Failed to subscribe to {}: {}", channel_name, error);
                return;
            }
            let mut stream = pubsub.on_message();

            loop {
                tokio::select! {
                    msg_result = stream.next() => {
                        match msg_result {
                            Some(msg) => {
                                let payload: String = msg.get_payload().unwrap_or_default();
                                let channel_name = msg.get_channel_name().to_string();
                                let pubsub_msg = PubSubMessage { channel: channel_name, payload, pattern: None };
                                if let Err(e) = tx_clone.send(pubsub_msg).await {
                                    error!("Failed to send message to channel: {}", e);
                                    break;
                                }
                            }
                            None => {
                                debug!("PubSub stream ended for channel {}", channel_name);
                                break;
                            }
                        }
                    }
                    _ = shutdown.recv() => {
                        info!("Shutting down subscription for channel {}", channel_name);
                        break;
                    }
                }
            }
        });

        Ok((rx, handle))
    }
}
