// RadixIP Axum adapter
use axum::{
    extract::Request,
    middleware::Next,
    response::{Response, IntoResponse},
    http::{StatusCode, HeaderMap},
    Json,
};
use std::net::IpAddr;
use std::sync::Arc;
use radixip_rs::Engine;
use serde::{Deserialize, Serialize};


#[derive(Clone)]
pub struct RadixIPMiddleware {
    engine: Arc<Engine>,
    classifier: Arc<Classifier>,
    config: Arc<Config>,
}

impl RadixIPMiddleware {
    pub fn new(engine: Engine, config: Config) -> Self {
        Self {
            engine: Arc::new(engine),
            classifier: Arc::new(Classifier::new(config.clone())),
            config: Arc::new(config),
        }
    }
}