use std::net::SocketAddr;
use std::str::FromStr;
use std::sync::Arc;
use std::time::Instant;

use hyper::service::{make_service_fn, service_fn};
use hyper::{Body, Request, Response, Server};
use ipnetwork::IpNetwork;
use lazy_static::lazy_static;
use prometheus::{
    Counter, CounterVec, Encoder, Gauge, Histogram, Opts, Registry, TextEncoder,
    register_counter, register_counter_vec, register_gauge, register_histogram,
};
use tonic::{Request as TonicRequest, Response as TonicResponse, Status, transport::Server as TonicServer};

use radixip::engine::EngineWrapper;
use radixip::traits::{EngineVariant, NodeVariant};
use radixip::types::Metadata;

pub mod pb {
    tonic::include_proto!("radixip.v1");
}

use pb::radix_service_server::{RadixService, RadixServiceServer};
use pb::*;

lazy_static! {
    static ref REGISTRY: Registry = Registry::new();
    static ref LOOKUPS_TOTAL: CounterVec = register_counter_vec!(
        Opts::new("radixip_lookups_total", "Total number of IP lookups performed"),
        &["result"]
    )
    .unwrap();
    static ref INSERTS_TOTAL: CounterVec = register_counter_vec!(
        Opts::new("radixip_inserts_total", "Total number of prefix insertions"),
        &["status"]
    )
    .unwrap();
    static ref REMOVALS_TOTAL: Counter = register_counter!(
        Opts::new("radixip_removals_total", "Total number of prefix removals")
    )
    .unwrap();
    static ref LOOKUP_DURATION: Histogram = register_histogram!(
        "radixip_lookup_duration_seconds",
        "Duration of IP lookup requests in seconds"
    )
    .unwrap();
    static ref ACTIVE_ROUTES: Gauge = register_gauge!(
        Opts::new("radixip_active_routes", "Current total number of active routes in tree")
    )
    .unwrap();
}

#[derive(Clone)]
pub struct RadixServiceImpl {
    engine: Arc<EngineWrapper>,
}

impl RadixServiceImpl {
    pub fn new() -> Self {
        let engine = EngineWrapper::new_with_tree(
            EngineVariant::Standard,
            NodeVariant::CompressedAtomic,
            true,
        );
        Self {
            engine: Arc::new(engine),
        }
    }
}

#[tonic::async_trait]
impl RadixService for RadixServiceImpl {
    async fn insert(
        &self,
        request: TonicRequest<InsertRequest>,
    ) -> Result<TonicResponse<InsertResponse>, Status> {
        let req = request.into_inner();
        let prefix: IpNetwork = match req.prefix.parse() {
            Ok(p) => p,
            Err(e) => {
                INSERTS_TOTAL.with_label_values(&["error"]).inc();
                return Ok(TonicResponse::new(InsertResponse {
                    success: false,
                    is_new: false,
                    error_message: e.to_string(),
                }));
            }
        };

        let mut meta = Metadata::new(
            req.metadata
                .as_ref()
                .map(|m| m.value.clone())
                .unwrap_or_default(),
        );
        if let Some(ref metadata) = req.metadata {
            for (k, v) in &metadata.attributes {
                meta.with_attribute(k, v);
            }
        }

        match self.engine.insert(prefix, meta) {
            Ok(_) => {
                INSERTS_TOTAL.with_label_values(&["success"]).inc();
                ACTIVE_ROUTES.set(self.engine.size() as f64);
                Ok(TonicResponse::new(InsertResponse {
                    success: true,
                    is_new: true,
                    error_message: String::new(),
                }))
            }
            Err(e) => {
                INSERTS_TOTAL.with_label_values(&["error"]).inc();
                Ok(TonicResponse::new(InsertResponse {
                    success: false,
                    is_new: false,
                    error_message: e,
                }))
            }
        }
    }

    async fn lookup(
        &self,
        request: TonicRequest<LookupRequest>,
    ) -> Result<TonicResponse<LookupResponse>, Status> {
        let start = Instant::now();
        let req = request.into_inner();
        let ip: std::net::IpAddr = match req.ip.parse() {
            Ok(ip) => ip,
            Err(_) => {
                LOOKUPS_TOTAL.with_label_values(&["invalid"]).inc();
                LOOKUP_DURATION.observe(start.elapsed().as_secs_f64());
                return Ok(TonicResponse::new(LookupResponse {
                    found: false,
                    metadata: None,
                }));
            }
        };

        let result = self.engine.lookup(&ip);
        LOOKUP_DURATION.observe(start.elapsed().as_secs_f64());

        match result {
            Some(meta) => {
                LOOKUPS_TOTAL.with_label_values(&["hit"]).inc();
                Ok(TonicResponse::new(LookupResponse {
                    found: true,
                    metadata: Some(pb::Metadata {
                        value: meta.value,
                        attributes: meta.attributes,
                    }),
                }))
            }
            None => {
                LOOKUPS_TOTAL.with_label_values(&["miss"]).inc();
                Ok(TonicResponse::new(LookupResponse {
                    found: false,
                    metadata: None,
                }))
            }
        }
    }

    async fn remove(
        &self,
        request: TonicRequest<RemoveRequest>,
    ) -> Result<TonicResponse<RemoveResponse>, Status> {
        let req = request.into_inner();
        let prefix: IpNetwork = match req.prefix.parse() {
            Ok(p) => p,
            Err(_) => {
                return Ok(TonicResponse::new(RemoveResponse {
                    found: false,
                    metadata: None,
                }));
            }
        };

        let removed = self.engine.remove(&prefix);
        if let Some(ref meta) = removed {
            REMOVALS_TOTAL.inc();
            ACTIVE_ROUTES.set(self.engine.size() as f64);
            Ok(TonicResponse::new(RemoveResponse {
                found: true,
                metadata: Some(pb::Metadata {
                    value: meta.value.clone(),
                    attributes: meta.attributes.clone(),
                }),
            }))
        } else {
            Ok(TonicResponse::new(RemoveResponse {
                found: false,
                metadata: None,
            }))
        }
    }

    async fn contains(
        &self,
        request: TonicRequest<ContainsRequest>,
    ) -> Result<TonicResponse<ContainsResponse>, Status> {
        let req = request.into_inner();
        let prefix: IpNetwork = match req.prefix.parse() {
            Ok(p) => p,
            Err(_) => return Ok(TonicResponse::new(ContainsResponse { contains: false })),
        };
        Ok(TonicResponse::new(ContainsResponse {
            contains: self.engine.contains(&prefix),
        }))
    }

    async fn clear(
        &self,
        _request: TonicRequest<ClearRequest>,
    ) -> Result<TonicResponse<ClearResponse>, Status> {
        self.engine.clear();
        ACTIVE_ROUTES.set(0.0);
        Ok(TonicResponse::new(ClearResponse { success: true }))
    }

    async fn get_stats(
        &self,
        _request: TonicRequest<StatsRequest>,
    ) -> Result<TonicResponse<StatsResponse>, Status> {
        let stats = self.engine.stats();
        Ok(TonicResponse::new(StatsResponse {
            inserts: stats.inserts as i64,
            lookups: stats.lookups as i64,
            hits: stats.hits as i64,
            misses: stats.misses as i64,
            removals: stats.removals as i64,
            size: stats.size as i64,
        }))
    }

    async fn stream_insert(
        &self,
        request: TonicRequest<tonic::Streaming<InsertRequest>>,
    ) -> Result<TonicResponse<StreamInsertResponse>, Status> {
        let mut stream = request.into_inner();
        let mut count = 0u64;

        while let Some(req) = stream.message().await? {
            if let Ok(prefix) = req.prefix.parse::<IpNetwork>() {
                let mut meta = Metadata::new(
                    req.metadata
                        .as_ref()
                        .map(|m| m.value.clone())
                        .unwrap_or_default(),
                );
                if let Some(ref metadata) = req.metadata {
                    for (k, v) in &metadata.attributes {
                        meta.with_attribute(k, v);
                    }
                }
                if self.engine.insert(prefix, meta).is_ok() {
                    count += 1;
                }
            }
        }

        ACTIVE_ROUTES.set(self.engine.size() as f64);
        Ok(TonicResponse::new(StreamInsertResponse {
            inserted_count: count,
        }))
    }
}

async fn metrics_service(
    _req: Request<Body>,
) -> Result<Response<Body>, hyper::Error> {
    let encoder = TextEncoder::new();
    let metric_families = prometheus::gather();
    let mut buffer = vec![];
    encoder.encode(&metric_families, &mut buffer).unwrap();

    Ok(Response::builder()
        .status(200)
        .header("Content-Type", encoder.format_type())
        .body(Body::from(buffer))
        .unwrap())
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let grpc_port = std::env::var("GRPC_PORT").unwrap_or_else(|_| "50052".to_string());
    let metrics_port = std::env::var("METRICS_PORT").unwrap_or_else(|_| "9091".to_string());

    // Spawn Prometheus Metrics Server
    let metrics_addr: SocketAddr = SocketAddr::from_str(&format!("0.0.0.0:{}", metrics_port))?;
    tokio::spawn(async move {
        let make_svc = make_service_fn(|_conn| async {
            Ok::<_, hyper::Error>(service_fn(metrics_service))
        });
        println!("[Rust gRPC Server] Metrics endpoint listening on http://{}/metrics", metrics_addr);
        if let Err(e) = Server::bind(&metrics_addr).serve(make_svc).await {
            eprintln!("Metrics server error: {}", e);
        }
    });

    // Start Tonic gRPC Server
    let grpc_addr: SocketAddr = SocketAddr::from_str(&format!("0.0.0.0:{}", grpc_port))?;
    let service = RadixServiceImpl::new();

    let config_path = std::env::var("RADIXIP_CONFIG")
        .or_else(|_| std::env::var("CONFIG_PATH"))
        .unwrap_or_else(|_| "radixip.yaml".to_string());

    println!("[Rust gRPC Server] RadixIP gRPC service listening on gRPC://{}", grpc_addr);

    if std::path::Path::new(&config_path).exists() {
        println!("[Rust gRPC Server] 🔥 RadixIP policy hot-reloader active watching {}", config_path);
        let watcher = Arc::new(radixip_policy::ConfigWatcher::new(&config_path)?);
        let engine_dyn: Arc<Box<dyn radixip::RadixEngine>> = Arc::new(Box::new(radixip::engine::EngineWrapper::new_with_tree(
            EngineVariant::Standard,
            NodeVariant::CompressedAtomic,
            true,
        )));
        let interceptor = radixip_grpc_interceptor::from_yaml::GrpcWatchedRadixIpInterceptor::new(watcher, engine_dyn);

        TonicServer::builder()
            .layer(tonic::service::interceptor(interceptor))
            .add_service(RadixServiceServer::new(service))
            .serve(grpc_addr)
            .await?;
    } else {
        println!("[Rust gRPC Server] ℹ️ Config file {} not found. Running without policy interceptors.", config_path);
        TonicServer::builder()
            .add_service(RadixServiceServer::new(service))
            .serve(grpc_addr)
            .await?;
    }

    Ok(())
}
