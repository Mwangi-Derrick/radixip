use std::sync::Arc;
use tokio::signal;

use actix_web::{web, App, HttpServer};
use axum::{routing::get, Router};
use tonic::transport::Server;
// use tower::ServiceBuilder; // For tower standalone

use radixip::{new_high_performance, RadixEngine};
use radixip_config::RadixIpConfig;
use radixip_policy::watcher::ConfigWatcher;

use radixip_actix::ActixWatchedRadixIpMiddleware;
use radixip_axum::AxumWatchedRadixIpLayer;
use radixip_grpc_interceptor::GrpcWatchedRadixIpLayer;

pub mod pb {
    tonic::include_proto!("radixip.v1");
}

#[derive(Default)]
struct KitchenGrpcService;

#[tonic::async_trait]
impl pb::radix_service_server::RadixService for KitchenGrpcService {
    async fn insert(
        &self,
        _request: tonic::Request<pb::InsertRequest>,
    ) -> Result<tonic::Response<pb::InsertResponse>, tonic::Status> {
        Err(tonic::Status::unimplemented("kitchen-sink Insert is not implemented"))
    }

    async fn lookup(
        &self,
        _request: tonic::Request<pb::LookupRequest>,
    ) -> Result<tonic::Response<pb::LookupResponse>, tonic::Status> {
        Ok(tonic::Response::new(pb::LookupResponse {
            found: false,
            metadata: None,
        }))
    }

    async fn remove(
        &self,
        _request: tonic::Request<pb::RemoveRequest>,
    ) -> Result<tonic::Response<pb::RemoveResponse>, tonic::Status> {
        Err(tonic::Status::unimplemented("kitchen-sink Remove is not implemented"))
    }

    async fn contains(
        &self,
        _request: tonic::Request<pb::ContainsRequest>,
    ) -> Result<tonic::Response<pb::ContainsResponse>, tonic::Status> {
        Err(tonic::Status::unimplemented("kitchen-sink Contains is not implemented"))
    }

    async fn clear(
        &self,
        _request: tonic::Request<pb::ClearRequest>,
    ) -> Result<tonic::Response<pb::ClearResponse>, tonic::Status> {
        Err(tonic::Status::unimplemented("kitchen-sink Clear is not implemented"))
    }

    async fn get_stats(
        &self,
        _request: tonic::Request<pb::StatsRequest>,
    ) -> Result<tonic::Response<pb::StatsResponse>, tonic::Status> {
        Err(tonic::Status::unimplemented("kitchen-sink GetStats is not implemented"))
    }

    async fn stream_insert(
        &self,
        _request: tonic::Request<tonic::Streaming<pb::InsertRequest>>,
    ) -> Result<tonic::Response<pb::StreamInsertResponse>, tonic::Status> {
        Err(tonic::Status::unimplemented("kitchen-sink StreamInsert is not implemented"))
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    println!("🚀 Starting Rust Kitchen Sink Test App");

    let config_path = "config/radixip.yaml";
    let _initial_config = RadixIpConfig::from_file(config_path)?;

    // 1. Initialize Shared RadixIP Engine
    let radix_engine = new_high_performance().await;
    let radix_engine: Arc<Box<dyn RadixEngine>> = Arc::new(radix_engine);

    // 3. Config Watcher (hot reloading)
    let watcher = Arc::new(ConfigWatcher::new_with_engine(
        config_path,
        Some(radix_engine.clone()),
    )?);

    // Prepare servers
    let (tx, _rx) = tokio::sync::broadcast::channel(1);

    // Axum Server (9081)
    let axum_watcher = watcher.clone();
    let axum_engine = radix_engine.clone();
    let tx_axum = tx.clone();
    let axum_task = tokio::spawn(async move {
        let app = Router::new()
            .route("/health", get(|| async { "ok" }))
            .route("/api/v1/public", get(|| async { "axum public ok" }))
            .route("/api/v1/auth", get(|| async { "axum auth get ok" }))
            .route("/api/v1/auth", axum::routing::post(|| async { "axum auth post ok" }))
            .layer(AxumWatchedRadixIpLayer::new(axum_watcher, axum_engine));

        let listener = tokio::net::TcpListener::bind("0.0.0.0:9081").await.unwrap();
        println!("🍅 Axum listening on :9081");

        let mut rx = tx_axum.subscribe();
        axum::serve(listener, app)
            .with_graceful_shutdown(async move {
                let _ = rx.recv().await;
                println!("Shutting down Axum...");
            })
            .await
            .unwrap();
    });

    // Actix-Web Server (9082)
    let actix_watcher = watcher.clone();
    let actix_engine = radix_engine.clone();
    let _tx_actix = tx.clone();
    let actix_server = HttpServer::new(move || {
        App::new()
            .wrap(ActixWatchedRadixIpMiddleware::new(
                actix_watcher.clone(),
                actix_engine.clone(),
            ))
            .route("/health", web::get().to(|| async { "ok" }))
            .route(
                "/api/v1/public",
                web::get().to(|| async { "actix public ok" }),
            )
            .route(
                "/api/v1/auth", 
                web::get().to(|| async { "actix auth get ok" })
            )
            .route(
                "/api/v1/auth", 
                web::post().to(|| async { "actix auth post ok" })
            )
    })
    .bind("0.0.0.0:9082")?
    .run();
    println!("🎭 Actix-Web listening on :9082");

    let actix_handle = actix_server.handle();
    let actix_task = tokio::spawn(async move {
        actix_server.await.unwrap();
    });

    let mut rx_actix = tx.subscribe();
    tokio::spawn(async move {
        let _ = rx_actix.recv().await;
        println!("Shutting down Actix...");
        actix_handle.stop(true).await;
    });

    // gRPC Server (50052)
    // For simplicity, we just bind a dummy service or use the Layer on an empty router
    let grpc_watcher = watcher.clone();
    let grpc_engine = radix_engine.clone();
    let tx_grpc = tx.clone();
    let grpc_task = tokio::spawn(async move {
        let addr = "0.0.0.0:50052".parse().unwrap();
        println!("📞 Tonic gRPC listening on :50052");

        let mut rx = tx_grpc.subscribe();
        Server::builder()
            .layer(GrpcWatchedRadixIpLayer::new(grpc_watcher, grpc_engine))
            .add_service(pb::radix_service_server::RadixServiceServer::new(
                KitchenGrpcService,
            ))
            .serve_with_shutdown(addr, async move {
                let _ = rx.recv().await;
                println!("Shutting down gRPC...");
            })
            .await
            .unwrap();
    });

    // Wait for shutdown signal
    match signal::ctrl_c().await {
        Ok(()) => {
            println!("\nShutdown signal received. Stopping servers...");
            let _ = tx.send(());
        }
        Err(err) => {
            eprintln!("Unable to listen for shutdown signal: {}", err);
        }
    }

    // Join tasks
    let _ = tokio::join!(axum_task, actix_task, grpc_task);
    println!("Done.");

    Ok(())
}
