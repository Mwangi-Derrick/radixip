# RadixIP Middleware

RadixIP provides drop-in middleware for popular Go and Rust web frameworks. It integrates the **high-performance Radix Tree Blocklist** and the **lock-free Token Bucket Rate Limiter** directly into your HTTP request lifecycle.

## Supported Frameworks

### Go
- **Gin**: `github.com/Mwangi-Derrick/radixip/lib/go/adapters/gin`
- **Fiber**: `github.com/Mwangi-Derrick/radixip/lib/go/adapters/fiber`
- **Echo**: `github.com/Mwangi-Derrick/radixip/lib/go/adapters/echo`
- **gRPC**: `github.com/Mwangi-Derrick/radixip/lib/go/adapters/grpc-interceptor`

### Rust
- **Axum**: `radixip-axum`
- **Actix-Web**: `radixip-actix`
- **Tower** (Generic): `radixip-tower`
- **gRPC (Tonic)**: `radixip-grpc-interceptor`

## Hot-Reloading Configuration (Zero Downtime)

The absolute best way to use RadixIP middleware is via the **`FromYAML`** helpers. These helpers read a `radixip.yaml` file on startup and spawn a background filesystem watcher. Whenever you edit the YAML file, the middleware **atomically hot-swaps** its configuration and rate-limiters with zero downtime and zero lock contention.

### Example: Gin (Go)

```go
package main

import (
	"log"

	"github.com/gin-gonic/gin"
	radixipgin "github.com/Mwangi-Derrick/radixip/lib/go/adapters/gin"
	radixip_engine "github.com/Mwangi-Derrick/radixip/lib/go/engine"
)

type EngineAdapter struct {
	inner *radixip_engine.EngineWrapper
}

func (a *EngineAdapter) Lookup(ipStr string) bool {
    // Implement string -> net.IP -> engine.Lookup
    return false // your impl here
}

func main() {
	r := gin.Default()

    // 1. Setup your blocklist engine (state is separate from config)
    engine := &EngineAdapter{/* ... */}

    // 2. Attach hot-reloading middleware
	mw, stop, err := radixipgin.NewFromYAML("radixip.yaml", engine)
	if err != nil {
		log.Fatalf("Failed to load RadixIP config: %v", err)
	}
	defer stop() // Clean up fsnotify on shutdown

	r.Use(mw)

	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	r.Run(":8080")
}
```

### Example: Axum (Rust)

```rust
use axum::{routing::get, Router};
use radixip_axum::AxumWatchedRadixIpLayer;
use radixip_policy::ConfigWatcher;
use std::sync::Arc;

#[tokio::main]
async fn main() {
    // 1. Setup blocklist engine
    let engine = Arc::new(radix_engine); // Box<dyn RadixEngine>

    // 2. Setup ConfigWatcher
    let watcher = Arc::new(ConfigWatcher::new("radixip.yaml").unwrap());

    // 3. Attach middleware
    let app = Router::new()
        .route("/", get(|| async { "Hello, World!" }))
        .layer(AxumWatchedRadixIpLayer::new(watcher, engine));

    axum::Server::bind(&"0.0.0.0:8080".parse().unwrap())
        .serve(app.into_make_service())
        .await
        .unwrap();
}
```

## `radixip.yaml` Schema

Both Go and Rust share the exact same YAML configuration schema.

```yaml
radixip:
  engine:
    variant: "concurrent"
    node_variant: "atomic"
    num_shards: 16
    cache:
      enabled: true
      max_entries: 10000
      ttl_seconds: 3600

  middleware:
    ip_source: "x-forwarded-for"
    trusted_proxies:
      - "10.0.0.0/8"
      - "172.16.0.0/12"
      - "192.168.0.0/16"
    responses:
      blocked: 403      # HTTP status when IP is in blocklist
      rate_limited: 429 # HTTP status when token bucket is empty

  blocklist:
    enabled: true
    sources:
      - type: "file"
        path: "/etc/radixip/blocklist.txt"

  rate_limit:
    enabled: true
    algorithm: "token_bucket"
    capacity: 100        # Max burst size
    refill_rate: 10      # Tokens added per second
    max_buckets: 1000000 # Max unique IPs tracked
    ttl_seconds: 60      # Evict IPs idle for >60s
    bucket_mode:
      mode: "ip"         # "ip", "subnet", or "both"
      depth_v4: 24
      depth_v6: 48
    # Metrics endpoints configuration
  metrics:
      enabled: true
      endpoint: "/metrics"       # Prometheus metrics endpoint
    pprof:                   # Go pprof endpoints for debugging
      enabled: true
      routes: ["cpu", "heap", "goroutine", "threadcreate", "block", "mutex"]

    # Per-IP Flagging & Auto-Banning
  auto_ban:
    enabled: true
    threshold_violations: 5    # 5 rate-limit 429 violations within window
    window_seconds: 10         # Sliding window size in seconds
    ban_duration_seconds: 30   # 30 second temporary ban (sweeper test uses 35s wait)

  # Per-API Route Policies (Longest Prefix Route Matching)
  rate_limit_routes:
    enabled: true
    routes:
      - path: "/api/v1/auth"
        methods: ["POST", "PUT"]
        rate_limit:
          capacity: 5
          refill_rate: 1
          enabled: true
        - path: "/api/v1/public"
          methods: ["GET"]
          rate_limit:
            capacity: 1000
            refill_rate: 100
            enabled: true
```

## IP Extraction & Security

The middleware automatically attempts to extract the client IP from the following sources, in order:
1. `X-Forwarded-For` (parsed right-to-left, skipping IPs in `trusted_proxies`)
2. `X-Real-IP`
3. The raw network connection `Remote-Addr`

If a request contains a spoofed `X-Forwarded-For` like `8.8.8.8, 192.168.1.100` and `192.168.1.0/24` is in `trusted_proxies`, RadixIP will correctly identify `8.8.8.8` as the true client IP.

## gRPC Interceptors

### Go gRPC Interceptor

```go
package main

import (
	"log"
	"google.golang.org/grpc"
	radixipgrpc "github.com/Mwangi-Derrick/radixip/lib/go/adapters/grpc-interceptor"
)

func main() {
	unary, stream, stop, err := radixipgrpc.NewFromYAML("radixip.yaml", engineAdapter)
	if err != nil {
		log.Fatalf("Failed to initialize RadixIP gRPC interceptor: %v", err)
	}
	defer stop()

	srv := grpc.NewServer(
		grpc.UnaryInterceptor(unary),
		grpc.StreamInterceptor(stream),
	)
	// Register services and serve...
}
```

### Rust gRPC (Tonic) Interceptor

```rust
use radixip_grpc_interceptor::from_yaml::{GrpcWatchedRadixIpInterceptor, GrpcWatchedRadixIpLayer};
use radixip_policy::ConfigWatcher;
use std::sync::Arc;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let watcher = Arc::new(ConfigWatcher::new("radixip.yaml")?);
    let engine = Arc::new(my_radix_engine);

    // Option A: Tonic Interceptor (metadata-only)
    let interceptor = GrpcWatchedRadixIpInterceptor::new(watcher.clone(), engine.clone());
    let svc = tonic::service::interceptor(my_grpc_service, interceptor);

    // Option B: Tower Layer
    let layer = GrpcWatchedRadixIpLayer::new(watcher, engine);
    tonic::transport::Server::builder()
        .layer(layer)
        .add_service(my_grpc_service)
        .serve(addr)
        .await?;

    Ok(())
}
```

## 🐳 Docker Sidecar Deployment

RadixIP can be deployed as an independent, high-performance sidecar service (like Kong or Prometheus).

### Quick Start with Docker

```bash
docker run -d \
  --name radixip-sidecar \
  -p 50051:50051 \
  -p 9090:9090 \
  -v $(pwd)/radixip.yaml:/etc/radixip/radixip.yaml:ro \
  ghcr.io/mwangi-derrick/radixip/sidecar:latest
```

When deployed in Kubernetes or Docker Compose, any modification to the mounted `radixip.yaml` volume is **automatically detected and hot-reloaded** by the background watcher without restarting the container.

