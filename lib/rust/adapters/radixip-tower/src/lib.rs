//! Tower Layer and Service for RadixIP Middleware.
//!
//! Extracts the IP, evaluates the RadixIP `PolicyEngine`, and either
//! allows the request through or short-circuits with a 403/429 response.

use futures_util::future::BoxFuture;
use http::{Request, Response, StatusCode};
use radixip_config::ResponseConfig;
use radixip_policy::{PolicyDecision, PolicyEngine};
use std::net::SocketAddr;
use std::sync::Arc;
use std::task::{Context, Poll};
use tower::{Layer, Service};

// ---------------------------------------------------------------------------
// Layer
// ---------------------------------------------------------------------------

/// Tower Layer for RadixIP.
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
    type Service = RadixIpMiddleware<S>;

    fn layer(&self, inner: S) -> Self::Service {
        RadixIpMiddleware {
            inner,
            engine: self.engine.clone(),
            responses: self.responses.clone(),
        }
    }
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

/// Tower Service for RadixIP.
#[derive(Clone)]
pub struct RadixIpMiddleware<S> {
    inner: S,
    engine: Arc<PolicyEngine>,
    responses: Arc<ResponseConfig>,
}

impl<S, ReqBody, ResBody> Service<Request<ReqBody>> for RadixIpMiddleware<S>
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
            // Extract headers.
            let headers = req.headers();
            let xff = headers
                .get("x-forwarded-for")
                .and_then(|v| v.to_str().ok());
            let x_real_ip = headers
                .get("x-real-ip")
                .and_then(|v| v.to_str().ok());

            // Extract remote addr if the extension was provided (e.g., by axum's ConnectInfo).
            let remote_addr = req.extensions().get::<SocketAddr>();

            let decision = engine.check(xff, x_real_ip, remote_addr);

            match decision {
                PolicyDecision::Allow => {
                    // Pass to next service
                    inner.call(req).await
                }
                PolicyDecision::Block => {
                    let mut res = Response::new(ResBody::from(r#"{"error":"blocked"}"#.to_string()));
                    *res.status_mut() = StatusCode::from_u16(responses.blocked)
                        .unwrap_or(StatusCode::FORBIDDEN);
                    res.headers_mut().insert(
                        http::header::CONTENT_TYPE,
                        http::header::HeaderValue::from_static("application/json"),
                    );
                    Ok(res)
                }
                PolicyDecision::Limit => {
                    let mut res = Response::new(ResBody::from(r#"{"error":"rate limited"}"#.to_string()));
                    *res.status_mut() = StatusCode::from_u16(responses.rate_limited)
                        .unwrap_or(StatusCode::TOO_MANY_REQUESTS);
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
                    let body = format!(r#"{{"error": "bad request: {}"}}"#, msg);
                    let mut res = Response::new(ResBody::from(body));
                    *res.status_mut() = StatusCode::BAD_REQUEST;
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
