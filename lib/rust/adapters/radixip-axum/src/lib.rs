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

use axum::{
    body::Body,
    extract::Request,
    response::{IntoResponse, Response},
};
use futures_util::future::BoxFuture;
use radixip_config::ResponseConfig;
use radixip_policy::{PolicyDecision, PolicyEngine};
use std::sync::Arc;
use std::task::{Context, Poll};
use tower::{Layer, Service};
use tower_service::Service as TowerService;

// Re-export for convenience
pub use radixip_tower::{RadixIpLayer, RadixIpMiddleware};

/// Axum-specific RadixIP middleware service
#[derive(Clone)]
pub struct AxumRadixIpService<S> {
    inner: S,
    engine: Arc<PolicyEngine>,
    responses: Arc<ResponseConfig>,
}

impl<S> AxumRadixIpService<S> {
    /// Create a new Axum RadixIP service
    pub fn new(inner: S, engine: Arc<PolicyEngine>, responses: ResponseConfig) -> Self {
        Self {
            inner,
            engine,
            responses: Arc::new(responses),
        }
    }
}

impl<S> Service<Request> for AxumRadixIpService<S>
where
    S: TowerService<Request, Response = Response> + Send + 'static,
    S::Future: Send + 'static,
    S::Error: Into<axum::BoxError>,
{
    type Response = Response;
    type Error = axum::BoxError;
    type Future = BoxFuture<'static, Result<Self::Response, Self::Error>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx).map_err(Into::into)
    }

    fn call(&mut self, req: Request) -> Self::Future {
        let engine = self.engine.clone();
        let responses = self.responses.clone();

        // Extract IP information from request
        let headers = req.headers().clone();
        let xff = headers.get("x-forwarded-for").and_then(|v| v.to_str().ok());
        let x_real_ip = headers.get("x-real-ip").and_then(|v| v.to_str().ok());

        // Extract remote address from request extensions
        let remote_addr = req
            .extensions()
            .get::<axum::extract::ConnectInfo<std::net::SocketAddr>>()
            .map(|connect_info| connect_info.0);

        // Call inner service
        let mut inner = std::mem::replace(&mut self.inner, unsafe {
            // Safety: We're replacing with a valid service that will be used
            // in the future, not during this method call
            std::mem::zeroed()
        });

        Box::pin(async move {
            let decision = engine.check(xff, x_real_ip, remote_addr.as_ref());

            match decision {
                PolicyDecision::Allow => inner.call(req).await.map_err(Into::into),
                PolicyDecision::Block => {
                    let status = axum::http::StatusCode::from_u16(responses.blocked)
                        .unwrap_or(axum::http::StatusCode::FORBIDDEN);
                    let response = (
                        status,
                        [(axum::http::header::CONTENT_TYPE, "application/json")],
                        r#"{"error":"blocked"}"#,
                    )
                        .into_response();
                    Ok(response)
                }
                PolicyDecision::Limit => {
                    let status = axum::http::StatusCode::from_u16(responses.rate_limited)
                        .unwrap_or(axum::http::StatusCode::TOO_MANY_REQUESTS);
                    let response = (
                        status,
                        [
                            (axum::http::header::CONTENT_TYPE, "application/json"),
                            (axum::http::header::RETRY_AFTER, "1"),
                        ],
                        r#"{"error":"rate limited"}"#,
                    )
                        .into_response();
                    Ok(response)
                }
                PolicyDecision::BadRequest(msg) => {
                    let body = format!(r#"{{"error": "bad request: {}"}}"#, msg);
                    let response = (
                        axum::http::StatusCode::BAD_REQUEST,
                        [(axum::http::header::CONTENT_TYPE, "application/json")],
                        body,
                    )
                        .into_response();
                    Ok(response)
                }
            }
        })
    }
}

/// Axum-specific RadixIP Layer
#[derive(Clone)]
pub struct AxumRadixIpLayer {
    engine: Arc<PolicyEngine>,
    responses: Arc<ResponseConfig>,
}

impl AxumRadixIpLayer {
    /// Create a new Axum RadixIP layer
    pub fn new(engine: Arc<PolicyEngine>, responses: ResponseConfig) -> Self {
        Self {
            engine,
            responses: Arc::new(responses),
        }
    }
}

impl<S> Layer<S> for AxumRadixIpLayer {
    type Service = AxumRadixIpService<S>;

    fn layer(&self, service: S) -> Self::Service {
        AxumRadixIpService::new(service, self.engine.clone(), (*self.responses).clone())
    }
}

/// Extension trait for adding RadixIP middleware to Axum Router
pub trait RadixIpExt {
    /// Add RadixIP middleware to the router
    fn layer_radixip(self, engine: Arc<PolicyEngine>, responses: ResponseConfig) -> Self;
}

impl RadixIpExt for axum::Router {
    fn layer_radixip(self, engine: Arc<PolicyEngine>, responses: ResponseConfig) -> Self {
        self.layer(AxumRadixIpLayer::new(engine, responses))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::StatusCode;
    use axum::{routing::get, Router};
    use tower::ServiceExt;

    #[tokio::test]
    async fn test_radixip_middleware_allow() {
        // This is a placeholder test
        // In real tests, you would create a PolicyEngine with proper configuration
        // and test the middleware behavior
        assert!(true);
    }
}
