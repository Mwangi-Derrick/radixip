use thiserror::Error;

#[derive(Debug, Error)]
pub enum RadixError {
    #[error("invalid CIDR prefix: {0}")]
    InvalidPrefix(String),
    #[error("invalid IP address: {0}")]
    InvalidIp(String),
    #[error("engine error: {0}")]
    Engine(String),
    #[error("serialization error: {0}")]
    Serialization(String),
}

pub type Result<T> = std::result::Result<T, RadixError>;
