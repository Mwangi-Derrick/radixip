//! Hot-reloading Tower layer for RadixIP.
//!
//! Wraps any Tower service with a `ConfigWatcher`-backed policy that hot-swaps
//! when the YAML configuration file changes (zero-downtime, wait-free reads).
//!
//! # Usage
//!
//! ```rust,ignore
//! use radixip_tower::from_yaml::TowerWatchedRadixIpLayer;
//! use radixip_policy::ConfigWatcher;
//! use std::sync::Arc;
//!
//! let watcher = Arc::new(ConfigWatcher::new("radixip.yaml")?);
//! let engine = Arc::new(my_engine); // Box<dyn RadixEngine>
//!
//! let app = tower::ServiceBuilder::new()
//!     .layer(TowerWatchedRadixIpLayer::new(watcher, engine))
//!     .service(my_inner_service);
//! ```

use futures_util::future::BoxFuture;
use http::{Request, Response};
use radixip::RadixEngine;
use radixip_policy::{extract_ip, ConfigWatcher};
use std::sync::Arc;
use std::task::{Context, Poll};
use tower::{Layer, Service};

/// Tower Layer backed by a `ConfigWatcher` — auto hot-swaps on YAML change.
#[derive(Clone)]
pub struct TowerWatchedRadixIpLayer {
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl TowerWatchedRadixIpLayer {
    pub fn new(watcher: Arc<ConfigWatcher>, engine: Arc<Box<dyn RadixEngine>>) -> Self {
        Self { watcher, engine }
    }
}

impl<S> Layer<S> for TowerWatchedRadixIpLayer {
    type Service = TowerWatchedRadixIpService<S>;

    fn layer(&self, service: S) -> Self::Service {
        TowerWatchedRadixIpService {
            inner: service,
            watcher: self.watcher.clone(),
            engine: self.engine.clone(),
        }
    }
}

/// Tower Service produced by `TowerWatchedRadixIpLayer`.
#[derive(Clone)]
pub struct TowerWatchedRadixIpService<S> {
    inner: S,
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl<S, ReqBody, ResBody> Service<Request<ReqBody>> for TowerWatchedRadixIpService<S>
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

        // Extract headers while we still have &req.
        let xff = req
            .headers()
            .get("x-forwarded-for")
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_owned());
        let x_real_ip = req
            .headers()
            .get("x-real-ip")
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_owned());
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
