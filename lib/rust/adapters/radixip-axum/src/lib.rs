//! Axum integration for RadixIP Middleware.
//!
//! This crate provides a convenience re-export of `radixip_tower::RadixIpLayer`
//! specifically tailored for `axum`.
//!
//! # Usage
//!
//! ```no_run
//! use axum::{routing::get, Router};
//! use radixip_axum::RadixIpLayer;
//! use radixip_policy::PolicyEngine;
//! use radixip_config::{MiddlewareConfig, RateLimitConfig};
//! use std::sync::Arc;
//!
//! // Assume engine is configured and instantiated
//! // let engine = PolicyEngine::new(radix_engine, middleware_config, rate_limit_config, true);
//! //
//! // let app = Router::new()
//! //     .route("/", get(|| async { "Hello, World!" }))
//! //     .layer(RadixIpLayer::new(Arc::new(engine), middleware_config.responses));
//! ```

pub use radixip_tower::{RadixIpLayer, RadixIpMiddleware};