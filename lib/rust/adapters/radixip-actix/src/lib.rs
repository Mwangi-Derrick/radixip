//! Actix-Web Middleware for RadixIP.
//!
//! Extracts the IP, evaluates the RadixIP `PolicyEngine`, and either
//! allows the request through or short-circuits with a 403/429 response.

pub mod from_yaml;
pub use from_yaml::{ActixWatchedRadixIpMiddleware, ActixWatchedRadixIpService};

use actix_web::{
    body::EitherBody,
    dev::{forward_ready, Service, ServiceRequest, ServiceResponse, Transform},
    http::header,
    Error, HttpResponse,
};
use futures_util::future::LocalBoxFuture;
use radixip_config::ResponseConfig;
use radixip_policy::{PolicyDecision, PolicyEngine};
use std::net::SocketAddr;
use std::rc::Rc;
use std::sync::Arc;

// ---------------------------------------------------------------------------
// Transform
// ---------------------------------------------------------------------------

/// Actix-Web Transform for RadixIP.
#[derive(Clone)]
pub struct RadixIpMiddleware {
    engine: Arc<PolicyEngine>,
    responses: Arc<ResponseConfig>,
}

impl RadixIpMiddleware {
    /// Create a new RadixIP middleware.
    pub fn new(engine: Arc<PolicyEngine>, responses: ResponseConfig) -> Self {
        Self {
            engine,
            responses: Arc::new(responses),
        }
    }
}

impl<S, B> Transform<S, ServiceRequest> for RadixIpMiddleware
where
    S: Service<ServiceRequest, Response = ServiceResponse<B>, Error = Error> + 'static,
    S::Future: 'static,
    B: 'static,
{
    type Response = ServiceResponse<EitherBody<B>>;
    type Error = Error;
    type InitError = ();
    type Transform = RadixIpService<S>;
    type Future = std::future::Ready<Result<Self::Transform, Self::InitError>>;

    fn new_transform(&self, service: S) -> Self::Future {
        std::future::ready(Ok(RadixIpService {
            service: Rc::new(service),
            engine: self.engine.clone(),
            responses: self.responses.clone(),
        }))
    }
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

/// Actix-Web Service for RadixIP.
pub struct RadixIpService<S> {
    service: Rc<S>,
    engine: Arc<PolicyEngine>,
    responses: Arc<ResponseConfig>,
}

impl<S, B> Service<ServiceRequest> for RadixIpService<S>
where
    S: Service<ServiceRequest, Response = ServiceResponse<B>, Error = Error> + 'static,
    S::Future: 'static,
    B: 'static,
{
    type Response = ServiceResponse<EitherBody<B>>;
    type Error = Error;
    type Future = LocalBoxFuture<'static, Result<Self::Response, Self::Error>>;

    forward_ready!(service);

    fn call(&self, req: ServiceRequest) -> Self::Future {
        let engine = self.engine.clone();
        let responses = self.responses.clone();
        let service = self.service.clone();

        Box::pin(async move {
            let headers = req.headers();
            let xff = headers.get("x-forwarded-for").and_then(|v| v.to_str().ok());
            let x_real_ip = headers.get("x-real-ip").and_then(|v| v.to_str().ok());

            let remote_addr = req.peer_addr().map(|mut a| {
                // Actix gives us SocketAddr, just pass it through
                a
            });

            let decision = engine.check(xff, x_real_ip, remote_addr.as_ref());

            match decision {
                PolicyDecision::Allow => {
                    let res = service.call(req).await?;
                    Ok(res.map_into_left_body())
                }
                PolicyDecision::Block | PolicyDecision::AutoBanned => {
                    let mut builder = HttpResponse::build(
                        actix_web::http::StatusCode::from_u16(responses.blocked)
                            .unwrap_or(actix_web::http::StatusCode::FORBIDDEN),
                    );
                    builder.insert_header((header::CONTENT_TYPE, "application/json"));
                    let res = builder.body(r#"{"error":"blocked","reason":"ip_blocked"}"#);
                    Ok(req.into_response(res.map_into_right_body()))
                }
                PolicyDecision::Limit => {
                    let mut builder = HttpResponse::build(
                        actix_web::http::StatusCode::from_u16(responses.rate_limited)
                            .unwrap_or(actix_web::http::StatusCode::TOO_MANY_REQUESTS),
                    );
                    builder.insert_header((header::CONTENT_TYPE, "application/json"));
                    builder.insert_header((header::RETRY_AFTER, "1"));
                    let res = builder.body(r#"{"error":"rate limited"}"#);
                    Ok(req.into_response(res.map_into_right_body()))
                }
                PolicyDecision::BadRequest(msg) => {
                    let mut builder = HttpResponse::BadRequest();
                    builder.insert_header((header::CONTENT_TYPE, "application/json"));
                    let body = format!(r#"{{"error": "bad request: {}"}}"#, msg);
                    let res = builder.body(body);
                    Ok(req.into_response(res.map_into_right_body()))
                }
            }
        })
    }
}
