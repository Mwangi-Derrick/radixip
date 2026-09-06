# RadixIP Middleware

RadixIP provides drop-in middleware for popular Go, Rust, Node.js, and Python web frameworks. It integrates the **high-performance Radix Tree Blocklist**, the **lock-free Token Bucket Rate Limiter**, route-specific policies, and configurable auto-ban directly into your request lifecycle.

The framework adapter is intentionally thin. Your framework remains responsible for routing and handler execution; RadixIP runs as an early request gate, evaluates one shared native policy, and either allows the request to continue or returns the configured denial response.

## How Integration Works

RadixIP does not scan your project or automatically attach itself to a framework. You install the normal framework package and explicitly register the RadixIP adapter in that framework's middleware pipeline.

```text
incoming request
  |
  v
framework middleware pipeline
  |
  +--> RadixIP adapter
  |       |
  |       +--> extract client IP
  |       +--> blocklist lookup
  |       +--> route/global token bucket
  |       +--> auto-ban tracker
  |       +--> allow, limit, or block
  |
  v
framework router and application handler
```

Node.js uses one `radixip` package with subpath adapters such as
`radixip/middleware`. Python uses one `radixip` package with framework modules
such as `radixip.middleware`. Express, Next.js, TanStack Start, and FastAPI
remain dependencies of the application. RadixIP does not install or replace
those frameworks.

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

### Node.js
- **Express**: `radixip/middleware` -> `radixipExpress`
- **Next.js**: `radixip/middleware` -> `radixipNext`
- **TanStack Start**: `radixip/middleware` -> `radixipTanStackStart`
- **Fastify**: use `RadixPolicy` from the native addon in a Fastify hook

### Python
- **FastAPI**: `radixip.middleware` -> `RadixIPMiddleware`
- **Starlette**: use the same ASGI middleware class directly
- **Flask/Django**: use `RadixPolicy.check_ip()` in the framework's request hook

The built-in adapters focus on the stack maintained by this project. Other
frameworks can use the same policy object without reimplementing rate limiting.

## Hot-Reloading Configuration (Zero Downtime)

The recommended server-side integration is the hot-reloading configuration path. Go and Rust `FromYAML` helpers read a `radixip.yaml` file on startup and spawn a background filesystem watcher. The Node and Python native policy bindings load the same schema into one process-level policy object; framework adapters reuse that object for every request.

Configuration-derived limiter and route state is replaced atomically when hot reload is enabled. The blocklist engine remains a shared object, and each process should create one policy instance rather than constructing a limiter per request.

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
  metrics:
    enabled: true
    prometheus_path: "/metrics"

  # Per-IP Flagging & Auto-Banning
  auto_ban:
    enabled: true
    threshold_violations: 5    # 5 rate-limit 429 violations within window
    window_seconds: 10         # Sliding window size in seconds
    ban_duration_seconds: 30   # Temporary ban duration

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

### Configuration ownership

The same conceptual policy is available in every language:

| Area | Responsibility |
|---|---|
| `middleware` | Client-IP source, trusted proxies, and response status codes |
| `blocklist` | Whether blocklist checks are active and where prefixes come from |
| `rate_limit` | Global token-bucket capacity, refill, bucket key, and eviction |
| `rate_limit_routes` | Longest-prefix route and method-specific limit overrides |
| `auto_ban` | Violation threshold, sliding window, and temporary ban duration |
| `metrics` | Metrics enablement and endpoint naming |

Go and Rust adapters can watch the YAML file directly. Python and Node should
keep one `RadixPolicy` instance alive for the process lifetime. A watcher-backed
policy object can be added later when applications need runtime configuration
reloads in those ecosystems.

## IP Extraction & Security

The middleware automatically attempts to extract the client IP from the following sources, in order:
1. `X-Forwarded-For` (parsed right-to-left, skipping IPs in `trusted_proxies`)
2. `X-Real-IP`
3. The raw network connection `Remote-Addr`

If a request contains a spoofed `X-Forwarded-For` like `8.8.8.8, 192.168.1.100` and `192.168.1.0/24` is in `trusted_proxies`, RadixIP will correctly identify `8.8.8.8` as the true client IP.

### Proxy trust by ecosystem

Forwarded headers are only trustworthy when the request came through a proxy
that your application controls. Configure proxy trust at the framework edge:

```js
// Express: use the narrowest setting that describes your deployment.
app.set("trust proxy", ["loopback", "10.0.0.0/8"]);
```

For Next.js and TanStack Start, pass an explicit `resolveIp(request)` function
to the adapter when a trusted proxy chain is present. The default does not
trust forwarded headers. FastAPI likewise defaults to the direct peer address;
pass `resolve_ip(request)` when an ingress proxy has been authenticated.

Never treat an arbitrary client-provided `X-Forwarded-For` value as the real
address. A bad proxy configuration can let an attacker evade both rate limits
and blocklists by changing the apparent client IP.

## Node.js Middleware

Install RadixIP in the application that owns the framework:

```bash
npm install radixip express
```

The framework is not bundled into RadixIP. The adapter is selected by an
explicit import, and the framework invokes the returned function during its
normal request pipeline.

### Express

```js
const express = require("express");
const { radixipExpress } = require("radixip/middleware");

const app = express();
app.set("trust proxy", ["loopback", "10.0.0.0/8"]);

app.use(radixipExpress({
  configPath: "config/radixip.yaml",
}));

app.get("/", (_req, res) => res.json({ ok: true }));
app.listen(3000);
```

Pass `policy` instead of `configPath` when the application already created a
native policy object:

```js
const { RadixPolicy } = require("radixip");
const policy = RadixPolicy.fromYaml("config/radixip.yaml");
app.use(radixipExpress({ policy }));
```

The adapter calls `next()` for `allow`, returns `403` for `block` or auto-ban,
and returns `429` with `Retry-After` for `limit`.

### Next.js

```ts
// middleware.ts
import { radixipNext } from "radixip/middleware";

export default radixipNext({
  configPath: "config/radixip.yaml",
});

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
```

For deployments behind a proxy, provide a resolver that implements the
deployment's trusted-hop rules. The adapter returns standard Web `Response`
objects, so it can be used in the Node runtime. The native addon is not
available in the Next.js Edge Runtime; use a Node runtime route or a Node
middleware deployment.

### TanStack Start

TanStack Start uses the Web request/response model, so the adapter follows the
same shape as Next.js:

```ts
import { radixipTanStackStart } from "radixip/middleware";

const radixipMiddleware = radixipTanStackStart({
  configPath: "config/radixip.yaml",
});
```

Register `radixipMiddleware` through the TanStack Start request middleware
hook used by the version of Start in your application. Supply `resolveIp` for
trusted ingress proxies. Keep the policy instance at module or application
scope, not inside the request function.

### Fastify and other Node frameworks

There is no need to create another native limiter for Fastify. Use one
`RadixPolicy` and call it from an `onRequest` or `preHandler` hook:

```js
const { RadixPolicy } = require("radixip");
const policy = RadixPolicy.fromYaml("config/radixip.yaml");

fastify.addHook("onRequest", async (request, reply) => {
  const result = policy.checkIp(request.ip);
  if (result.decision === "block") {
    return reply.code(403).send({ error: "blocked" });
  }
  if (result.decision === "limit") {
    return reply
      .header("Retry-After", result.retryAfterSeconds || 1)
      .code(429)
      .send({ error: "rate limited" });
  }
});
```

## Python Middleware

Install the optional framework dependency in the application environment:

```bash
pip install "radixip[fastapi]"
```

### FastAPI

```python
from fastapi import FastAPI
from radixip import RadixPolicy
from radixip.middleware import RadixIPMiddleware

app = FastAPI()
policy = RadixPolicy.from_yaml("config/radixip.yaml")

app.add_middleware(RadixIPMiddleware, policy=policy)

@app.get("/")
async def root():
    return {"ok": True}
```

The default middleware uses `request.client.host` and does not trust forwarded
headers. Provide a resolver for a trusted ingress:

```python
def resolve_ip(request):
    # Only do this after authenticating the proxy chain.
    return request.headers.get("x-forwarded-for", "").split(",")[0].strip()

app.add_middleware(
    RadixIPMiddleware,
    policy=policy,
    resolve_ip=resolve_ip,
)
```

### Flask, Django, and other Python frameworks

Use the same process-level policy object in the framework's request hook:

```python
result = policy.check_ip(client_ip)
if result["decision"] == "block":
    return {"error": "blocked"}, 403
if result["decision"] == "limit":
    return {"error": "rate limited"}, 429, {
        "Retry-After": str(result["retry_after_seconds"] or 1)
    }
```

This keeps enforcement behavior identical across FastAPI, Flask, Django, and
custom ASGI/WSGI adapters.

## Decision and Status Mapping

All built-in adapters use the same policy result:

| Policy result | HTTP behavior | gRPC behavior |
|---|---|---|
| `allow` | Continue to the next handler | Invoke the RPC handler |
| `block` | `403 Forbidden` | `PermissionDenied` |
| `limit` | `429 Too Many Requests` plus `Retry-After` | `ResourceExhausted` plus retry metadata |
| `bad_request` | `400 Bad Request` | `InvalidArgument` |

Auto-banned clients use the block response because the temporary ban is
enforced by the same policy path. Do not add a second local limiter in the
framework adapter: doing so can consume tokens twice and make behavior diverge
between languages.

## Native Policy and FFI Boundary

The Rust policy engine owns the expensive shared state:

- radix-tree blocklist lookups
- token-bucket maps and atomic updates
- route-trie matching
- violation windows and auto-ban state
- configuration validation

Python and Node call the native policy binding with one IP and receive a small
decision object. C and C++ can use the packed-IP policy ABI in
`lib/rust/ffi/radixip_policy.h`. The request path should not cross the
boundary with YAML, JSON, or a newly allocated policy object on every request.

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

