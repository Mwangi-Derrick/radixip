use axum::{
    body::Body,
    extract::Request,
    response::{IntoResponse, Response},
};
use futures_util::future::BoxFuture;
use radixip::RadixEngine;
use radixip_policy::{extract_ip, ConfigWatcher};
use std::sync::Arc;
use std::task::{Context, Poll};
use tower::{Layer, Service};
use tower_service::Service as TowerService;

/// Axum service for hot-reloading RadixIP configuration
#[derive(Clone)]
pub struct AxumWatchedRadixIpService<S> {
    inner: S,
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl<S> AxumWatchedRadixIpService<S> {
    pub fn new(inner: S, watcher: Arc<ConfigWatcher>, engine: Arc<Box<dyn RadixEngine>>) -> Self {
        Self {
            inner,
            watcher,
            engine,
        }
    }
}

impl<S> Service<Request> for AxumWatchedRadixIpService<S>
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
        let watcher = self.watcher.clone();
        let engine = self.engine.clone();

        // Extract IP information from request - convert to owned strings
        let headers = req.headers().clone();
        let xff = headers
            .get("x-forwarded-for")
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_string());
        let x_real_ip = headers
            .get("x-real-ip")
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_string());

        let remote_addr = req
            .extensions()
            .get::<axum::extract::ConnectInfo<std::net::SocketAddr>>()
            .map(|connect_info| connect_info.0);

        // Call inner service
        let mut inner = std::mem::replace(&mut self.inner, unsafe { std::mem::zeroed() });

        Box::pin(async move {
            let state = watcher.state(); // Wait-free pointer load
            let mw_cfg = &state.config.radixip.middleware;
            let rl_cfg = &state.config.radixip.rate_limit;
            let bl_cfg = &state.config.radixip.blocklist;
            let responses = &mw_cfg.responses;

            // 1. Extract IP
            let ip = match extract_ip(
                xff.as_deref(),
                x_real_ip.as_deref(),
                remote_addr.as_ref(),
                &mw_cfg.trusted_proxies,
            ) {
                Ok(ip) => ip,
                Err(e) => {
                    let body = format!(r#"{{"error": "bad request: {}"}}"#, e);
                    let response = (
                        axum::http::StatusCode::BAD_REQUEST,
                        [(axum::http::header::CONTENT_TYPE, "application/json")],
                        body,
                    )
                        .into_response();
                    return Ok(response);
                }
            };

            // 2. Blocklist check
            if bl_cfg.enabled && engine.lookup(&ip).is_some() {
                let status = axum::http::StatusCode::from_u16(responses.blocked)
                    .unwrap_or(axum::http::StatusCode::FORBIDDEN);
                let response = (
                    status,
                    [(axum::http::header::CONTENT_TYPE, "application/json")],
                    r#"{"error":"blocked"}"#,
                )
                    .into_response();
                return Ok(response);
            }

            // 3. Rate limit check
            if rl_cfg.enabled && !state.limiter.allow(ip, Some(engine.as_ref().as_ref())) {
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
                return Ok(response);
            }

            // Allow
            inner.call(req).await.map_err(Into::into)
        })
    }
}

/// Axum layer for hot-reloading RadixIP from a `ConfigWatcher`.
#[derive(Clone)]
pub struct AxumWatchedRadixIpLayer {
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl AxumWatchedRadixIpLayer {
    pub fn new(watcher: Arc<ConfigWatcher>, engine: Arc<Box<dyn RadixEngine>>) -> Self {
        Self { watcher, engine }
    }
}

impl<S> Layer<S> for AxumWatchedRadixIpLayer {
    type Service = AxumWatchedRadixIpService<S>;

    fn layer(&self, service: S) -> Self::Service {
        AxumWatchedRadixIpService::new(service, self.watcher.clone(), self.engine.clone())
    }
}
