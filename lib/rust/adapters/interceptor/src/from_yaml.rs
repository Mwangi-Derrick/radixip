//! Hot-reloading gRPC interceptor for RadixIP.
//!
//! Watches a YAML configuration file and atomically hot-swaps rate-limit
//! parameters and middleware options when the file changes (zero downtime).
//!
//! # Usage
//!
//! ```rust,ignore
//! use radixip_grpc_interceptor::from_yaml::{GrpcWatchedRadixIpInterceptor, GrpcWatchedRadixIpLayer};
//! use radixip_policy::ConfigWatcher;
//! use std::sync::Arc;
//!
//! let watcher = Arc::new(ConfigWatcher::new("radixip.yaml")?);
//! let engine = Arc::new(my_radix_engine); // Box<dyn RadixEngine>
//!
//! // Option A — tonic Interceptor trait (metadata-only, cheapest)
//! let interceptor = GrpcWatchedRadixIpInterceptor::new(watcher.clone(), engine.clone());
//! let svc = tonic::service::interceptor(my_service, interceptor);
//!
//! // Option B — Tower Layer (full HTTP/2 request)
//! let layer = GrpcWatchedRadixIpLayer::new(watcher, engine);
//! let server = tonic::transport::Server::builder()
//!     .layer(layer)
//!     .add_service(my_service)
//!     .serve(addr)
//!     .await?;
//! ```

use std::sync::Arc;
use std::task::{Context, Poll};

use futures_util::future::BoxFuture;
use http::{Request, Response};
use std::str::FromStr;
use tonic::{metadata::MetadataValue, service::Interceptor, Status};
use tower::{Layer, Service};

use radixip::RadixEngine;
use radixip_policy::{extract_ip, ConfigWatcher};

// Interceptor with Hot-Reload (tonic::service::Interceptor)

/// Tonic `Interceptor` that hot-reloads its config from a YAML watcher.
///
/// Every RPC reads the current config snapshot via an atomic pointer load
/// (wait-free). When the file changes, the watcher rebuilds the policy state
/// in the background and the next call picks it up automatically.
#[derive(Clone)]
pub struct GrpcWatchedRadixIpInterceptor {
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl GrpcWatchedRadixIpInterceptor {
    /// Create a new hot-reloading RadixIP interceptor.
    pub fn new(watcher: Arc<ConfigWatcher>, engine: Arc<Box<dyn RadixEngine>>) -> Self {
        Self { watcher, engine }
    }
}

impl Interceptor for GrpcWatchedRadixIpInterceptor {
    fn call(&mut self, req: tonic::Request<()>) -> Result<tonic::Request<()>, Status> {
        let state = self.watcher.state(); // Wait-free atomic pointer load
        let mw_cfg = &state.config.radixip.middleware;
        let rl_cfg = &state.config.radixip.rate_limit;
        let bl_cfg = &state.config.radixip.blocklist;

        let metadata = req.metadata();

        // 1. Extract client IP from metadata headers.
        let xff = metadata
            .get("x-forwarded-for")
            .and_then(|v| v.to_str().ok());
        let x_real_ip = metadata.get("x-real-ip").and_then(|v| v.to_str().ok());
        // tonic sets remote_addr via Request::remote_addr()
        let remote_addr = req.remote_addr();

        let ip = match extract_ip(
            xff,
            x_real_ip,
            remote_addr.as_ref(),
            &mw_cfg.trusted_proxies,
        ) {
            Ok(ip) => ip,
            Err(e) => {
                return Err(Status::invalid_argument(format!(
                    "failed to extract IP: {}",
                    e
                )));
            }
        };

        // 2. Blocklist check.
        if bl_cfg.enabled && self.engine.lookup(&ip).is_some() {
            return Err(Status::permission_denied("blocked: IP is in blocklist"));
        }

        // 3. Rate limit check.
        if rl_cfg.enabled && !state.limiter.allow(ip, Some(self.engine.as_ref().as_ref())) {
            let mut status = Status::resource_exhausted("rate limited: exceeded rate limit");
            let retry_after =
                MetadataValue::from_str("1").unwrap_or_else(|_| MetadataValue::from_static("1"));
            status.metadata_mut().insert("retry-after", retry_after);
            return Err(status);
        }

        Ok(req)
    }
}

// Tower Layer with Hot-Reload (http::Request<B> level)

/// Tower `Layer` that applies hot-reloading RadixIP checks to every HTTP/2 request.
///
/// Prefer this over `GrpcWatchedRadixIpInterceptor` when you need access to
/// request extensions (e.g., `SocketAddr` injected by `TcpIncoming`) or when
/// composing with other Tower middleware.
#[derive(Clone)]
pub struct GrpcWatchedRadixIpLayer {
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl GrpcWatchedRadixIpLayer {
    /// Create a new hot-reloading RadixIP layer.
    pub fn new(watcher: Arc<ConfigWatcher>, engine: Arc<Box<dyn RadixEngine>>) -> Self {
        Self { watcher, engine }
    }
}

impl<S> Layer<S> for GrpcWatchedRadixIpLayer {
    type Service = GrpcWatchedRadixIpService<S>;

    fn layer(&self, service: S) -> Self::Service {
        GrpcWatchedRadixIpService {
            inner: service,
            watcher: self.watcher.clone(),
            engine: self.engine.clone(),
        }
    }
}

/// Tower service produced by `GrpcWatchedRadixIpLayer`.
#[derive(Clone)]
pub struct GrpcWatchedRadixIpService<S> {
    inner: S,
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl<S, ReqBody, ResBody> Service<Request<ReqBody>> for GrpcWatchedRadixIpService<S>
where
    S: Service<Request<ReqBody>, Response = Response<ResBody>> + Clone + Send + 'static,
    S::Future: Send + 'static,
    S::Error: Send + 'static,
    ReqBody: Send + 'static,
    ResBody: From<String> + Send + 'static,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = BoxFuture<'static, Result<Self::Response, Self::Error>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Request<ReqBody>) -> Self::Future {
        let mut inner = self.inner.clone();
        let watcher = self.watcher.clone();
        let engine = self.engine.clone();

        // Extract headers before moving req into the async block.
        let xff = req
            .headers()
            .get("x-forwarded-for")
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_string());
        let x_real_ip = req
            .headers()
            .get("x-real-ip")
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_string());
        let remote_addr = req.extensions().get::<std::net::SocketAddr>().copied();

        Box::pin(async move {
            let state = watcher.state(); // Wait-free atomic pointer load
            let mw_cfg = &state.config.radixip.middleware;
            let rl_cfg = &state.config.radixip.rate_limit;
            let bl_cfg = &state.config.radixip.blocklist;
            let responses = &mw_cfg.responses;

            // 1. Extract IP.
            let ip = match extract_ip(
                xff.as_deref(),
                x_real_ip.as_deref(),
                remote_addr.as_ref(),
                &mw_cfg.trusted_proxies,
            ) {
                Ok(ip) => ip,
                Err(e) => {
                    let body = ResBody::from(format!(r#"{{"error":"bad request: {}"}}"#, e));
                    let mut res = Response::new(body);
                    *res.status_mut() = http::StatusCode::BAD_REQUEST;
                    res.headers_mut().insert(
                        http::header::CONTENT_TYPE,
                        http::header::HeaderValue::from_static("application/json"),
                    );
                    return Ok(res);
                }
            };

            // 2. Blocklist check.
            if bl_cfg.enabled && engine.lookup(&ip).is_some() {
                let status = http::StatusCode::from_u16(responses.blocked)
                    .unwrap_or(http::StatusCode::FORBIDDEN);
                let body = ResBody::from(r#"{"error":"blocked"}"#.to_string());
                let mut res = Response::new(body);
                *res.status_mut() = status;
                res.headers_mut().insert(
                    http::header::CONTENT_TYPE,
                    http::header::HeaderValue::from_static("application/json"),
                );
                return Ok(res);
            }

            // 3. Rate limit check.
            if rl_cfg.enabled && !state.limiter.allow(ip, Some(engine.as_ref().as_ref())) {
                let status = http::StatusCode::from_u16(responses.rate_limited)
                    .unwrap_or(http::StatusCode::TOO_MANY_REQUESTS);
                let body = ResBody::from(r#"{"error":"rate limited"}"#.to_string());
                let mut res = Response::new(body);
                *res.status_mut() = status;
                res.headers_mut().insert(
                    http::header::CONTENT_TYPE,
                    http::header::HeaderValue::from_static("application/json"),
                );
                res.headers_mut().insert(
                    http::header::RETRY_AFTER,
                    http::header::HeaderValue::from_static("1"),
                );
                return Ok(res);
            }

            inner.call(req).await
        })
    }
}
