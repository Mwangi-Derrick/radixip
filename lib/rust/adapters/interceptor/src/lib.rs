//! gRPC Interceptor for RadixIP using Tonic.
//!
//! Extracts the IP from gRPC metadata, evaluates the RadixIP `PolicyEngine`,
//! and either allows the request through or returns a gRPC error with appropriate
//! status codes (PermissionDenied for blocklist, ResourceExhausted for rate limiting).

pub mod from_yaml;
pub use from_yaml::{GrpcWatchedRadixIpInterceptor, GrpcWatchedRadixIpService};

use std::pin::Pin;
use std::sync::Arc;
use std::task::{Context, Poll};

use tonic::{
    body::BoxBody,
    metadata::{Ascii, MetadataValue},
    service::{Interceptor, InterceptorFn},
    transport::server::DecodeContext,
    Code, Request, Response, Status,
};

use radixip_config::ResponseConfig;
use radixip_policy::{PolicyDecision, PolicyEngine};

// ---------------------------------------------------------------------------
// Interceptor
// ---------------------------------------------------------------------------

/// Tonic interceptor for RadixIP.
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

    /// Convert to a tonic interceptor function.
    pub fn into_interceptor(self) -> InterceptorFn {
        let interceptor = self;
        InterceptorFn::new(move |mut req: Request<()>| {
            let decision = interceptor.check_request(&req);

            match decision {
                PolicyDecision::Allow => Ok(req),
                PolicyDecision::Block => {
                    Err(Status::permission_denied("blocked: IP is in blocklist"))
                }
                PolicyDecision::Limit => {
                    let mut status =
                        Status::resource_exhausted("rate limited: exceeded rate limit");
                    // Add retry-after metadata
                    let retry_after = MetadataValue::from_str("1")
                        .unwrap_or_else(|_| MetadataValue::from_static("1"));
                    status.metadata_mut().insert("retry-after", retry_after);
                    Err(status)
                }
                PolicyDecision::BadRequest(msg) => Err(Status::invalid_argument(format!(
                    "failed to extract IP: {}",
                    msg
                ))),
            }
        })
    }

    /// Check the request and return a policy decision.
    fn check_request(&self, req: &Request<()>) -> PolicyDecision {
        let metadata = req.metadata();

        // Extract X-Forwarded-For
        let xff = metadata
            .get("x-forwarded-for")
            .and_then(|v| v.to_str().ok());

        // Extract X-Real-IP
        let x_real_ip = metadata.get("x-real-ip").and_then(|v| v.to_str().ok());

        // Extract peer address from extensions
        let remote_addr = req.extensions().get::<std::net::SocketAddr>().copied();

        self.engine.check(xff, x_real_ip, remote_addr.as_ref())
    }
}

// ---------------------------------------------------------------------------
// Service Interceptor (Alternative approach using Tower Service)
// ---------------------------------------------------------------------------

use tower::Layer;
use tower::Service as TowerService;

/// Layer that wraps a tonic service with RadixIP interceptor logic.
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

/// Tower service that wraps a tonic service with RadixIP logic.
#[derive(Clone)]
pub struct RadixIpService<S> {
    inner: S,
    engine: Arc<PolicyEngine>,
    responses: Arc<ResponseConfig>,
}

impl<S, Req> TowerService<Req> for RadixIpService<S>
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
        let engine = self.engine.clone();
        let responses = self.responses.clone();

        Box::pin(async move {
            // Extract metadata and peer info from the request
            let metadata = req.metadata();
            let xff = metadata
                .get("x-forwarded-for")
                .and_then(|v| v.to_str().ok());
            let x_real_ip = metadata.get("x-real-ip").and_then(|v| v.to_str().ok());

            let remote_addr = req.extensions().get::<std::net::SocketAddr>().copied();

            let decision = engine.check(xff, x_real_ip, remote_addr.as_ref());

            match decision {
                PolicyDecision::Allow => service.call(req).await,
                PolicyDecision::Block => {
                    Err(Status::permission_denied("blocked: IP is in blocklist"))
                }
                PolicyDecision::Limit => {
                    let mut status =
                        Status::resource_exhausted("rate limited: exceeded rate limit");
                    let retry_after = MetadataValue::from_str("1")
                        .unwrap_or_else(|_| MetadataValue::from_static("1"));
                    status.metadata_mut().insert("retry-after", retry_after);
                    Err(status)
                }
                PolicyDecision::BadRequest(msg) => Err(Status::invalid_argument(format!(
                    "failed to extract IP: {}",
                    msg
                ))),
            }
        })
    }
}
