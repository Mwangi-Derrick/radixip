//! Hot-reloading gRPC interceptor for RadixIP using fsnotify.
//!
//! This module provides a tonic interceptor that watches a YAML configuration file
//! and hot-swaps rate-limit parameters and middleware options when the file changes.
//!
//! # Usage
//!
//! ```rust,ignore
//! use radixip_grpc::from_yaml::{GrpcWatchedRadixIpInterceptor, GrpcWatchedRadixIpLayer};
//! use radixip_policy::ConfigWatcher;
//! use std::sync::Arc;
//!
//! let watcher = ConfigWatcher::new("radixip.yaml")?;
//! let watcher = Arc::new(watcher);
//! let engine = Arc::new(my_radix_engine);
//!
//! // Using interceptor
//! let interceptor = GrpcWatchedRadixIpInterceptor::new(watcher.clone(), engine.clone());
//! let svc = MyService::new();
//! let svc_with_interceptor = tonic::service::interceptor(svc, interceptor.into_interceptor());
//!
//! // Or using layer
//! let layer = GrpcWatchedRadixIpLayer::new(watcher, engine);
//! let svc_with_interceptor = layer.layer(MyService::new());
//! ```

use std::pin::Pin;
use std::sync::atomic::{AtomicPtr, Ordering};
use std::sync::Arc;
use std::task::{Context, Poll};

use tonic::{metadata::MetadataValue, service::Interceptor, Code, Request, Response, Status};

use radixip_config::ConfigWatcher;
use radixip_policy::{PolicyDecision, PolicyEngine};
use tower::Layer;
use tower::Service as TowerService;

// ---------------------------------------------------------------------------
// Interceptor with Hot-Reload
// ---------------------------------------------------------------------------

/// Tonic interceptor for hot-reloading RadixIP configuration.
#[derive(Clone)]
pub struct GrpcWatchedRadixIpInterceptor {
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn radixip::RadixEngine>>,
}

impl GrpcWatchedRadixIpInterceptor {
    /// Create a new hot-reloading RadixIP interceptor.
    pub fn new(watcher: Arc<ConfigWatcher>, engine: Arc<Box<dyn radixip::RadixEngine>>) -> Self {
        Self { watcher, engine }
    }

    /// Convert to a tonic interceptor function.
    pub fn into_interceptor(self) -> tonic::service::InterceptorFn {
        let interceptor = self;
        tonic::service::InterceptorFn::new(move |mut req: Request<()>| {
            let state = interceptor.watcher.state(); // Wait-free pointer load
            let mw_cfg = &state.config.radixip.middleware;
            let rl_cfg = &state.config.radixip.rate_limit;
            let bl_cfg = &state.config.radixip.blocklist;
            let responses = &mw_cfg.responses;

            let metadata = req.metadata();

            // 1. Extract IP
            let xff = metadata
                .get("x-forwarded-for")
                .and_then(|v| v.to_str().ok());
            let x_real_ip = metadata.get("x-real-ip").and_then(|v| v.to_str().ok());
            let remote_addr = req.extensions().get::<std::net::SocketAddr>().copied();

            let ip = match radixip_policy::extract_ip(
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

            // 2. Blocklist check
            if bl_cfg.enabled {
                if let Some(_) = interceptor.engine.lookup(&ip) {
                    return Err(Status::permission_denied("blocked: IP is in blocklist"));
                }
            }

            // 3. Rate limit check
            if rl_cfg.enabled
                && !state
                    .limiter
                    .allow(ip, Some(interceptor.engine.as_ref().as_ref()))
            {
                let mut status = Status::resource_exhausted("rate limited: exceeded rate limit");
                let retry_after = MetadataValue::from_str("1")
                    .unwrap_or_else(|_| MetadataValue::from_static("1"));
                status.metadata_mut().insert("retry-after", retry_after);
                return Err(status);
            }

            Ok(req)
        })
    }

    /// Check the request and return a policy decision (used internally).
    fn check_request(&self, req: &Request<()>) -> PolicyDecision {
        let state = self.watcher.state();
        let mw_cfg = &state.config.radixip.middleware;
        let rl_cfg = &state.config.radixip.rate_limit;
        let bl_cfg = &state.config.radixip.blocklist;

        let metadata = req.metadata();
        let xff = metadata
            .get("x-forwarded-for")
            .and_then(|v| v.to_str().ok());
        let x_real_ip = metadata.get("x-real-ip").and_then(|v| v.to_str().ok());
        let remote_addr = req.extensions().get::<std::net::SocketAddr>().copied();

        let ip = match radixip_policy::extract_ip(
            xff,
            x_real_ip,
            remote_addr.as_ref(),
            &mw_cfg.trusted_proxies,
        ) {
            Ok(ip) => ip,
            Err(e) => return PolicyDecision::BadRequest(e.to_string()),
        };

        if bl_cfg.enabled {
            if let Some(_) = self.engine.lookup(&ip) {
                return PolicyDecision::Block;
            }
        }

        if rl_cfg.enabled && !state.limiter.allow(ip, Some(self.engine.as_ref().as_ref())) {
            return PolicyDecision::Limit;
        }

        PolicyDecision::Allow
    }
}

// ---------------------------------------------------------------------------
// Layer with Hot-Reload
// ---------------------------------------------------------------------------

/// Layer that wraps a tonic service with hot-reloading RadixIP interceptor logic.
#[derive(Clone)]
pub struct GrpcWatchedRadixIpLayer {
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn radixip::RadixEngine>>,
}

impl GrpcWatchedRadixIpLayer {
    /// Create a new hot-reloading RadixIP layer.
    pub fn new(watcher: Arc<ConfigWatcher>, engine: Arc<Box<dyn radixip::RadixEngine>>) -> Self {
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

/// Tower service that wraps a tonic service with hot-reloading RadixIP logic.
#[derive(Clone)]
pub struct GrpcWatchedRadixIpService<S> {
    inner: S,
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn radixip::RadixEngine>>,
}

impl<S, Req> TowerService<Req> for GrpcWatchedRadixIpService<S>
where
    S: TowerService<Req, Response = Response<BoxBody>, Error = Status> + Clone + Send + 'static,
    S::Future: Send,
    Req: tonic::service::GrpcRequestExt + Send + 'static,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future =
        Pin<Box<dyn std::future::Future<Output = Result<Self::Response, Self::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Req) -> Self::Future {
        let mut service = self.inner.clone();
        let watcher = self.watcher.clone();
        let engine = self.engine.clone();

        Box::pin(async move {
            let state = watcher.state();
            let mw_cfg = &state.config.radixip.middleware;
            let rl_cfg = &state.config.radixip.rate_limit;
            let bl_cfg = &state.config.radixip.blocklist;

            let metadata = req.metadata();
            let xff = metadata
                .get("x-forwarded-for")
                .and_then(|v| v.to_str().ok());
            let x_real_ip = metadata.get("x-real-ip").and_then(|v| v.to_str().ok());
            let remote_addr = req.extensions().get::<std::net::SocketAddr>().copied();

            // 1. Extract IP
            let ip = match radixip_policy::extract_ip(
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

            // 2. Blocklist check
            if bl_cfg.enabled {
                if let Some(_) = engine.lookup(&ip) {
                    return Err(Status::permission_denied("blocked: IP is in blocklist"));
                }
            }

            // 3. Rate limit check
            if rl_cfg.enabled && !state.limiter.allow(ip, Some(engine.as_ref().as_ref())) {
                let mut status = Status::resource_exhausted("rate limited: exceeded rate limit");
                let retry_after = MetadataValue::from_str("1")
                    .unwrap_or_else(|_| MetadataValue::from_static("1"));
                status.metadata_mut().insert("retry-after", retry_after);
                return Err(status);
            }

            service.call(req).await
        })
    }
}
