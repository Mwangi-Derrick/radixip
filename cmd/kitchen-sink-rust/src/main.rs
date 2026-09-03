use std::sync::Arc;
use tokio::signal;

use axum::{routing::get, Router};
use actix_web::{web, App, HttpServer};
use tonic::transport::Server;
// use tower::ServiceBuilder; // For tower standalone

use radixip::engine::{EngineVariant, NodeVariant};
use radixip_config::RadixIpConfig;
use radixip_policy::{PolicyEngine, watcher::ConfigWatcher};

use radixip_axum::AxumWatchedRadixIpLayer;
use radixip_actix::ActixWatchedRadixIpMiddleware;
use radixip_grpc_interceptor::GrpcWatchedRadixIpLayer;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    println!("🚀 Starting Rust Kitchen Sink Test App");

    let config_path = "../../config/radixip.yaml";
    let initial_config = RadixIpConfig::from_file(config_path)?;

    // 1. Initialize Shared RadixIP Engine
    let radix_engine = radixip::create_engine(EngineVariant::Concurrent, NodeVariant::Atomic);
    let radix_engine = Arc::new(radix_engine);

    // 2. Initialize Shared Policy Engine
    let policy_engine = Arc::new(PolicyEngine::new(
        radix_engine.clone(),
        initial_config.radixip.middleware.clone(),
        initial_config.radixip.rate_limit.clone(),
        initial_config.radixip.blocklist.enabled,
    ));

    // 3. Config Watcher (hot reloading)
    let watcher = Arc::new(ConfigWatcher::new(config_path, policy_engine.clone())?);

    // Prepare servers
    let (tx, mut rx) = tokio::sync::broadcast::channel(1);

    // Axum Server (9081)
    let axum_watcher = watcher.clone();
    let tx_axum = tx.clone();
    let axum_task = tokio::spawn(async move {
        let app = Router::new()
            .route("/api/v1/public", get(|| async { "axum public ok" }))
            .route("/api/v1/auth", get(|| async { "axum auth ok" }))
            .layer(AxumWatchedRadixIpLayer::new(axum_watcher));

        let listener = tokio::net::TcpListener::bind("0.0.0.0:9081").await.unwrap();
        println!("🍅 Axum listening on :9081");
        
        let mut rx = tx_axum.subscribe();
        axum::serve(listener, app)
            .with_graceful_shutdown(async move {
                let _ = rx.recv().await;
                println!("Shutting down Axum...");
            })
            .await.unwrap();
    });

    // Actix-Web Server (9082)
    let actix_watcher = watcher.clone();
    let tx_actix = tx.clone();
    let actix_server = HttpServer::new(move || {
        App::new()
            .wrap(ActixWatchedRadixIpMiddleware::new(actix_watcher.clone()))
            .route("/api/v1/public", web::get().to(|| async { "actix public ok" }))
            .route("/api/v1/auth", web::get().to(|| async { "actix auth ok" }))
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
    let tx_grpc = tx.clone();
    let grpc_task = tokio::spawn(async move {
        // We need a dummy service to attach the layer to. For now, we just bind and wait.
        // In a real app we'd add .add_service(MyGreeterServer::new(greeter))
        let addr = "0.0.0.0:50052".parse().unwrap();
        println!("📞 Tonic gRPC listening on :50052");
        
        let mut rx = tx_grpc.subscribe();
        Server::builder()
            .layer(GrpcWatchedRadixIpLayer::new(grpc_watcher))
            .serve_with_shutdown(addr, async move {
                let _ = rx.recv().await;
                println!("Shutting down Tonic...");
            })
            .await.unwrap();
    });

    // Wait for shutdown signal
    match signal::ctrl_c().await {
        Ok(()) => {
            println!("\nShutdown signal received. Stopping servers...");
            let _ = tx.send(());
        },
        Err(err) => {
            eprintln!("Unable to listen for shutdown signal: {}", err);
        },
    }

    // Join tasks
    let _ = tokio::join!(axum_task, actix_task, grpc_task);
    println!("Done.");

    Ok(())
}
