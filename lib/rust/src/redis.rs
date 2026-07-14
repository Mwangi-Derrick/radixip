use std::sync::Arc;
use std::time::Duration;
use redis::{
    aio::ConnectionManager,
    AsyncCommands, Client, RedisError,
};
use futures_util::StreamExt;
use tokio::sync::{mpsc, Mutex, broadcast};
use tokio::task::JoinHandle;
use tracing::{info, error, debug};

// Custom error type
#[derive(Debug, thiserror::Error)]
pub enum RedisPubSubError {
    #[error("Redis error: {0}")]
    Redis(#[from] RedisError),
    #[error("Channel send error")]
    SendError,
    #[error("Channel receive error")]
    RecvError,
}

type Result<T> = std::result::Result<T, RedisPubSubError>;

// Configuration for Redis connection
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

// Thread-safe Redis client wrapper
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

impl RedisClient {
    /// Create a new Redis client with connection pooling
    pub async fn new(config: RedisConfig) -> Result<Self> {
        let client = Client::open(config.url.clone())?;
        
        // Create connection manager with retry logic
        let connection_manager = ConnectionManager::new(client.clone())
            .await
            .map_err(|e| RedisPubSubError::Redis(e))?;

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

    /// Get a connection from the pool
    async fn get_connection(&self) -> Result<tokio::sync::MutexGuard<'_, ConnectionManager>> {
    Ok(self.inner.connection_manager.lock().await)
    }

        /// Publish a message to a channel
    pub async fn publish(&self, channel: &str, message: &str) -> Result<()> {
        let mut conn = self.get_connection().await?;
        let _: usize = conn.publish(channel, message).await?;
        debug!("Published to {}: {}", channel, message);
        Ok(())
    }

    /// Publish a JSON message
    pub async fn publish_json<T: serde::Serialize>(&self, channel: &str, data: &T) -> Result<()> {
        let json = serde_json::to_string(data)
            .map_err(|e| RedisPubSubError::Redis(RedisError::from((
                redis::ErrorKind::TypeError,
                "Serialization error",
                e.to_string(),
            ))))?;
        self.publish(channel, &json).await
    }

    /// Subscribe to a channel and process messages with a callback
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
                                
                                let pubsub_msg = PubSubMessage {
                                    channel: channel_name,
                                    payload,
                                    pattern: None,
                                };
                                
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

     /// Subscribe to a channel and send messages to an mpsc channel
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
                                
                                let pubsub_msg = PubSubMessage {
                                    channel: channel_name,
                                    payload,
                                    pattern: None,
                                };
                                
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

       /// Subscribe to a pattern (e.g., "news.*")
    pub async fn psubscribe(
        &self,
        pattern: &str,
    ) -> Result<(mpsc::Receiver<PubSubMessage>, JoinHandle<()>)> {
        let (tx, rx) = mpsc::channel(100);
        let tx_clone = tx.clone();

        let client = self.inner.client.clone();
        let shutdown_rx = self.inner.shutdown_tx.subscribe();
        let pattern_str = pattern.to_string();

        let handle = tokio::spawn(async move {
            let mut shutdown = shutdown_rx;
            let mut pubsub = match client.get_async_pubsub().await {
                Ok(pubsub) => pubsub,
                Err(error) => {
                    error!("Failed to create Redis Pub/Sub connection: {}", error);
                    return;
                }
            };
            if let Err(error) = pubsub.psubscribe(&pattern_str).await {
                error!("Failed to subscribe to pattern {}: {}", pattern_str, error);
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
                                let pattern = msg.get_pattern::<String>().ok();
                                
                                let pubsub_msg = PubSubMessage {
                                    channel: channel_name,
                                    payload,
                                    pattern,
                                };
                                
                                if let Err(e) = tx_clone.send(pubsub_msg).await {
                                    error!("Failed to send message to channel: {}", e);
                                    break;
                                }
                            }
                            None => {
                                debug!("PubSub stream ended for pattern {}", pattern_str);
                                break;
                            }
                        }
                    }
                    _ = shutdown.recv() => {
                        info!("Shutting down subscription for pattern {}", pattern_str);
                        break;
                    }
                }
            }
        });

        Ok((rx, handle))
    }

      /// Broadcast to all subscribers
    pub fn broadcast(&self) -> broadcast::Sender<PubSubMessage> {
        self.inner.pubsub_sender.clone()
    }

    /// Subscribe to broadcast messages
    pub fn subscribe_broadcast(&self) -> broadcast::Receiver<PubSubMessage> {
        self.inner.pubsub_sender.subscribe()
    }

    pub fn config(&self) -> &RedisConfig {
        &self.inner.config
    }

      /// Shutdown all subscriptions
    pub async fn shutdown(&self) -> Result<()> {
        let _ = self.inner.shutdown_tx.send(());
        Ok(())
    }

    /// Set a key-value pair
    pub async fn set(&self, key: &str, value: &str) -> Result<()> {
        let mut conn = self.get_connection().await?;
        let _: () = conn.set(key, value).await?;
        Ok(())
    }

    /// Get a value by key
    pub async fn get(&self, key: &str) -> Result<Option<String>> {
        let mut conn = self.get_connection().await?;
        let result: Option<String> = conn.get(key).await?;
        Ok(result)
    }
}
