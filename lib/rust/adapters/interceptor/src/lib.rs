//! gRPC Interceptor for RadixIP using Tonic.
//!
//! Extracts the IP from gRPC metadata, evaluates the RadixIP `PolicyEngine`,
//! and either allows the request through or returns a gRPC error with appropriate
//! status codes (PermissionDenied for blocklist, ResourceExhausted for rate limiting).
//!
//! # Usage (static config)
//!
//! ```rust,ignore
//! use radixip_grpc_interceptor::{RadixIpInterceptor, RadixIpLayer};
//! use radixip_policy::PolicyEngine;
//! use radixip_config::ResponseConfig;
//! use std::sync::Arc;
//!
//! let engine = Arc::new(PolicyEngine::new(/* ... */));
//! let responses = ResponseConfig::default();
//!
//! let interceptor = RadixIpInterceptor::new(engine.clone(), responses.clone());
//!
//! let svc = tonic::transport::Server::builder()
//!     .layer(RadixIpLayer::new(engine, responses))
//!     .add_service(my_service)
//!     .serve(addr)
//!     .await?;
//! ```

pub mod from_yaml;
pub use from_yaml::{GrpcWatchedRadixIpInterceptor, GrpcWatchedRadixIpLayer, GrpcWatchedRadixIpService};

use std::pin::Pin;
use std::sync::Arc;
use std::task::{Context, Poll};

use futures_util::future::BoxFuture;
use http::{Request, Response};
use tonic::{
    metadata::MetadataValue,
    service::Interceptor,
    Status,
};
use tower::{Layer, Service};

use radixip_config::ResponseConfig;
use radixip_policy::{PolicyDecision, PolicyEngine};

// ---------------------------------------------------------------------------
// Tonic Interceptor (used with `tonic::service::interceptor()`)
// ---------------------------------------------------------------------------

/// A tonic `Interceptor` that evaluates the RadixIP policy on every RPC.
///
/// The `Interceptor` trait in tonic operates on `Request<()>` (metadata-only)
/// **before** the body is read, which makes it the ideal place for blocklist
/// and rate-limit checks.
#[derive(Clone)]
pub struct RadixIpInterceptor {
    engine: Arc<PolicyEngine>,
    responses: Arc<ResponseConfig>,
}

impl RadixIpInterceptor {
    /// Create a new RadixIP interceptor.
    pub fn new(engine: Arc<PolicyEngine>, responses: ResponseConfig) -> Self {
        Self {
            engine,
            responses: Arc::new(responses),
        }
    }
}

impl Interceptor for RadixIpInterceptor {
    fn call(&mut self, mut req: tonic::Request<()>) -> Result<tonic::Request<()>, Status> {
        let metadata = req.metadata();

        // Extract X-Forwarded-For (gRPC clients send this as a metadata key).
        let xff = metadata
            .get("x-forwarded-for")
            .and_then(|v| v.to_str().ok());

        // Extract X-Real-IP.
        let x_real_ip = metadata.get("x-real-ip").and_then(|v| v.to_str().ok());

        // Extract the peer socket address injected by tonic's `TcpIncoming`.
        let remote_addr = req.remote_addr();

        let decision = self.engine.check(xff, x_real_ip, remote_addr.as_ref());

        match decision {
            PolicyDecision::Allow => Ok(req),
            PolicyDecision::Block => {
                Err(Status::permission_denied("blocked: IP is in blocklist"))
            }
            PolicyDecision::Limit => {
                let mut status = Status::resource_exhausted("rate limited: exceeded rate limit");
                let retry_after = MetadataValue::from_str("1")
                    .unwrap_or_else(|_| MetadataValue::from_static("1"));
                status.metadata_mut().insert("retry-after", retry_after);
                Err(status)
            }
            PolicyDecision::BadRequest(msg) => {
                Err(Status::invalid_argument(format!("failed to extract IP: {}", msg)))
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Tower Layer / Service (used when you need full body access or streaming)
// ---------------------------------------------------------------------------

/// Tower Layer that wraps a tonic service with RadixIP checks.
///
/// Unlike `RadixIpInterceptor`, this operates at the `http::Request<B>` level
/// so it composes naturally with the rest of the Tower middleware stack.
#[derive(Clone)]
pub struct RadixIpLayer {
    engine: Arc<PolicyEngine>,
    responses: Arc<ResponseConfig>,
}

impl RadixIpLayer {
    /// Create a new RadixIP layer.
    pub fn new(engine: Arc<PolicyEngine>, responses: ResponseConfig) -> Self {
        Self {
            engine,
            responses: Arc::new(responses),
        }
    }
}

impl<S> Layer<S> for RadixIpLayer {
    type Service = RadixIpService<S>;

    fn layer(&self, service: S) -> Self::Service {
        RadixIpService {
            inner: service,
            engine: self.engine.clone(),
            responses: self.responses.clone(),
        }
    }
}

/// Tower service that wraps a tonic service with RadixIP policy checks.
#[derive(Clone)]
pub struct RadixIpService<S> {
    inner: S,
    engine: Arc<PolicyEngine>,
    responses: Arc<ResponseConfig>,
}

impl<S, ReqBody, ResBody> Service<Request<ReqBody>> for RadixIpService<S>
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
        let engine = self.engine.clone();
        let responses = self.responses.clone();

        Box::pin(async move {
            // Extract metadata from the HTTP/2 headers (tonic sends metadata as headers).
            let headers = req.headers();
            let xff = headers
                .get("x-forwarded-for")
                .and_then(|v| v.to_str().ok());
            let x_real_ip = headers.get("x-real-ip").and_then(|v| v.to_str().ok());

            // Peer address is injected as an extension by tonic's transport layer.
            let remote_addr = req
                .extensions()
                .get::<std::net::SocketAddr>()
                .copied();

            let decision = engine.check(xff, x_real_ip, remote_addr.as_ref());

            match decision {
                PolicyDecision::Allow => inner.call(req).await,
                PolicyDecision::Block => {
                    let body = ResBody::from(r#"{"error":"blocked"}"#.to_string());
                    let mut res = Response::new(body);
                    *res.status_mut() = http::StatusCode::from_u16(responses.blocked)
                        .unwrap_or(http::StatusCode::FORBIDDEN);
                    res.headers_mut().insert(
                        http::header::CONTENT_TYPE,
                        http::header::HeaderValue::from_static("application/json"),
                    );
                    Ok(res)
                }
                PolicyDecision::Limit => {
                    let body = ResBody::from(r#"{"error":"rate limited"}"#.to_string());
                    let mut res = Response::new(body);
                    *res.status_mut() = http::StatusCode::from_u16(responses.rate_limited)
                        .unwrap_or(http::StatusCode::TOO_MANY_REQUESTS);
                    res.headers_mut().insert(
                        http::header::CONTENT_TYPE,
                        http::header::HeaderValue::from_static("application/json"),
                    );
                    res.headers_mut().insert(
                        http::header::RETRY_AFTER,
                        http::header::HeaderValue::from_static("1"),
                    );
                    Ok(res)
                }
                PolicyDecision::BadRequest(msg) => {
                    let body = ResBody::from(format!(r#"{{"error":"bad request: {}"}}"#, msg));
                    let mut res = Response::new(body);
                    *res.status_mut() = http::StatusCode::BAD_REQUEST;
                    res.headers_mut().insert(
                        http::header::CONTENT_TYPE,
                        http::header::HeaderValue::from_static("application/json"),
                    );
                    Ok(res)
                }
            }
        })
    }
}
