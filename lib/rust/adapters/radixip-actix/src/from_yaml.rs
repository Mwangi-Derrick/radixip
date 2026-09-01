use actix_web::{
    body::EitherBody,
    dev::{forward_ready, Service, ServiceRequest, ServiceResponse, Transform},
    http::header,
    Error, HttpResponse,
};
use futures_util::future::LocalBoxFuture;
use radixip::RadixEngine;
use radixip_policy::{extract_ip, ConfigWatcher};
use std::rc::Rc;
use std::sync::Arc;

/// Actix-Web Transform for hot-reloading RadixIP configuration.
#[derive(Clone)]
pub struct ActixWatchedRadixIpMiddleware {
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl ActixWatchedRadixIpMiddleware {
    pub fn new(watcher: Arc<ConfigWatcher>, engine: Arc<Box<dyn RadixEngine>>) -> Self {
        Self { watcher, engine }
    }
}

impl<S, B> Transform<S, ServiceRequest> for ActixWatchedRadixIpMiddleware
where
    S: Service<ServiceRequest, Response = ServiceResponse<B>, Error = Error> + 'static,
    S::Future: 'static,
    B: 'static,
{
    type Response = ServiceResponse<EitherBody<B>>;
    type Error = Error;
    type InitError = ();
    type Transform = ActixWatchedRadixIpService<S>;
    type Future = std::future::Ready<Result<Self::Transform, Self::InitError>>;

    fn new_transform(&self, service: S) -> Self::Future {
        std::future::ready(Ok(ActixWatchedRadixIpService {
            service: Rc::new(service),
            watcher: self.watcher.clone(),
            engine: self.engine.clone(),
        }))
    }
}

/// Actix-Web Service for hot-reloading RadixIP configuration.
pub struct ActixWatchedRadixIpService<S> {
    service: Rc<S>,
    watcher: Arc<ConfigWatcher>,
    engine: Arc<Box<dyn RadixEngine>>,
}

impl<S, B> Service<ServiceRequest> for ActixWatchedRadixIpService<S>
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
        let watcher = self.watcher.clone();
        let engine = self.engine.clone();
        let service = self.service.clone();

        Box::pin(async move {
            let state = watcher.state(); // Wait-free pointer load
            let mw_cfg = &state.config.radixip.middleware;
            let rl_cfg = &state.config.radixip.rate_limit;
            let bl_cfg = &state.config.radixip.blocklist;
            let responses = &mw_cfg.responses;

            let headers = req.headers();
            let xff = headers.get("x-forwarded-for").and_then(|v| v.to_str().ok());
            let x_real_ip = headers.get("x-real-ip").and_then(|v| v.to_str().ok());

            let remote_addr = req.peer_addr();

            // 1. Extract IP
            let ip = match extract_ip(
                xff,
                x_real_ip,
                remote_addr.as_ref(),
                &mw_cfg.trusted_proxies,
            ) {
                Ok(ip) => ip,
                Err(e) => {
                    let mut builder = HttpResponse::BadRequest();
                    builder.insert_header((header::CONTENT_TYPE, "application/json"));
                    let body = format!(r#"{{"error": "bad request: {}"}}"#, e);
                    let res = builder.body(body);
                    return Ok(req.into_response(res.map_into_right_body()));
                }
            };

            // 2. Blocklist check
            if bl_cfg.enabled && engine.lookup(&ip).is_some() {
                let mut builder = HttpResponse::build(
                    actix_web::http::StatusCode::from_u16(responses.blocked)
                        .unwrap_or(actix_web::http::StatusCode::FORBIDDEN),
                );
                builder.insert_header((header::CONTENT_TYPE, "application/json"));
                let res = builder.body(r#"{"error":"blocked"}"#);
                return Ok(req.into_response(res.map_into_right_body()));
            }

            // 3. Rate limit check
            if rl_cfg.enabled && !state.limiter.allow(ip, Some(engine.as_ref().as_ref())) {
                let mut builder = HttpResponse::build(
                    actix_web::http::StatusCode::from_u16(responses.rate_limited)
                        .unwrap_or(actix_web::http::StatusCode::TOO_MANY_REQUESTS),
                );
                builder.insert_header((header::CONTENT_TYPE, "application/json"));
                builder.insert_header((header::RETRY_AFTER, "1"));
                let res = builder.body(r#"{"error":"rate limited"}"#);
                return Ok(req.into_response(res.map_into_right_body()));
            }

            // Allow
            let res = service.call(req).await?;
            Ok(res.map_into_left_body())
        })
    }
}
