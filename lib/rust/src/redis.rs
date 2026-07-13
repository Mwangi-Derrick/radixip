use std::sync::Arc;
use std::time::Duration;
use redis::{
    aio::{ConnectionManager, PubSub},
    AsyncCommands, Client, RedisError, RedisResult,
};
use tokio::sync::{mpsc, Mutex, broadcast};
use tokio::task::JoinHandle;
use tracing::{info, error, warn, debug};

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
        let connection_manager = ConnectionManager::new(client)
            .await
            .map_err(|e| RedisPubSubError::Redis(e))?;

        let (pubsub_tx, _) = broadcast::channel(100);
        let (shutdown_tx, _) = broadcast::channel(1);

        let inner = RedisClientInner {
            connection_manager: Mutex::new(connection_manager),
            config,
            pubsub_sender: pubsub_tx,
            shutdown_tx,
        };

        Ok(Self {
            inner: Arc::new(inner),
        })
    }
