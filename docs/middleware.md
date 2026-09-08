# RadixIP Middleware

RadixIP provides drop-in middleware for popular Go, Rust, Node.js, and Python web frameworks. Each adapter integrates the **high-performance Radix Tree Blocklist**, the **lock-free Token Bucket Rate Limiter**, route-specific policies, and configurable auto-ban directly into the request lifecycle — in **~200 ns per request** from the policy check itself.

The framework adapter is intentionally thin. Your framework handles routing and handler execution; RadixIP runs as an early request gate, evaluates one shared native policy, and either allows the request to continue or returns the configured denial response.

---

## How Integration Works

RadixIP does not scan your project or automatically attach itself to a framework. You install the normal framework package and explicitly register the RadixIP adapter in that framework's middleware pipeline.

```text
incoming request
  │
  ▼
framework middleware pipeline
  │
  ├──► RadixIP adapter
  │       │
  │       ├──► 1. extract client IP (XFF → X-Real-IP → peer address)
  │       ├──► 2. blocklist lookup  (radix LPM, ~60 ns)
  │       ├──► 3. route-trie match  (per-path token bucket override)
  │       ├──► 4. token bucket      (global or per-route limiter)
  │       ├──► 5. auto-ban tracker  (sliding window violation counter)
  │       └──► allow → next handler
  │            limit → 429 + Retry-After
  │            block → 403
  │
  ▼
framework router + application handler
```

---

## Decision and Status Mapping

All built-in adapters use the same policy result:

| Policy decision | HTTP status | gRPC status |
|---|---|---|
| `allow` | Pass to next handler | Invoke the RPC handler |
| `block` | `403 Forbidden` | `PermissionDenied` |
| `auto_ban` | `403 Forbidden` | `PermissionDenied` |
| `limit` | `429 Too Many Requests` + `Retry-After` | `ResourceExhausted` + retry metadata |
| `bad_request` | `400 Bad Request` | `InvalidArgument` |

`block` and `auto_ban` both return `403` because the temporary ban is enforced by the same engine path. Do not add a second local limiter in the adapter — doing so can consume tokens twice and make behaviour diverge between languages.

---

## Supported Frameworks

### Go
| Framework | Import path |
|---|---|
| Gin | `github.com/Mwangi-Derrick/radixip/lib/go/adapters/gin` |
| Echo | `github.com/Mwangi-Derrick/radixip/lib/go/adapters/echo` |
| Fiber | `github.com/Mwangi-Derrick/radixip/lib/go/adapters/fiber` |
| gRPC | `github.com/Mwangi-Derrick/radixip/lib/go/adapters/grpc-interceptor` |

### Rust
| Crate | Description |
|---|---|
| `radixip-axum` | Axum `Layer` + hot-reload `ConfigWatcher` |
| `radixip-actix` | Actix-Web `Transform` + hot-reload |
| `radixip-tower` | Generic Tower `Layer` (works with any Tower-compatible server) |
| `radixip-grpc-interceptor` | Tonic gRPC — both `Interceptor` trait and Tower `Layer` variants |

### Node.js
| Framework | Export |
|---|---|
| Express | `radixip/middleware` → `radixipExpress` |
| Next.js (Node runtime) | `radixip/middleware` → `radixipNext` |
| TanStack Start | `radixip/middleware` → `radixipTanStackStart` |
| Fastify | `radixip/middleware` → `radixipFastify` / `radixipFastifyPlugin` |

### Python
| Framework | Import |
|---|---|
| FastAPI / Starlette | `from radixip.middleware import RadixIPMiddleware` |
| Flask | `from radixip.middleware import make_flask_hook` |
| Django | `from radixip.middleware import RadixIPDjangoMiddleware` |

---

## `radixip.yaml` Schema

Go and Rust share the exact same YAML configuration schema. Node and Python load the same file through their native policy binding.

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

  auto_ban:
    enabled: true
    threshold_violations: 5    # violations within the window before ban
    window_seconds: 10         # sliding window
    ban_duration_seconds: 3600 # temporary ban duration (1 hour)

  # Per-API route policies (longest-prefix path match)
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

  metrics:
    enabled: true
    prometheus_path: "/metrics"
```

### Configuration ownership

| Section | Controls |
|---|---|
| `middleware` | IP extraction source, trusted proxies, response status codes |
| `blocklist` | Static blocklist toggle and data sources |
| `rate_limit` | Global token-bucket capacity, refill rate, bucket key, eviction TTL |
| `rate_limit_routes` | Per-path per-method token-bucket overrides (route trie) |
| `auto_ban` | Violation threshold, sliding window, and temporary ban duration |
| `metrics` | Prometheus metrics endpoint |

---

## IP Extraction & Security

The middleware extracts the client IP in this order:

1. `X-Forwarded-For` — parsed **right-to-left**, skipping any IP in `trusted_proxies`
2. `X-Real-IP`
3. The raw network connection (`Remote-Addr` / peer address)

**Security warning:** Never trust an arbitrary client-provided `X-Forwarded-For` without configuring `trusted_proxies`. A misconfigured proxy trust allows a client to spoof any IP and bypass both rate limits and blocklists.

### Proxy trust by ecosystem

```js
// Express — use the narrowest setting that matches your deployment.
app.set('trust proxy', ['loopback', '10.0.0.0/8']);
```

For Next.js, TanStack Start, FastAPI, Flask, and Django, pass a `resolveIp` /
`resolve_ip` function that applies your deployment's trusted-hop rules.

---

## Go Middleware

### Gin

```go
package main

import (
    "log"

    "github.com/gin-gonic/gin"
    radixipgin "github.com/Mwangi-Derrick/radixip/lib/go/adapters/gin"
    radixip_engine "github.com/Mwangi-Derrick/radixip/lib/go/engine"
    "net"
)

// EngineAdapter satisfies the middleware Engine interface.
type EngineAdapter struct{ inner *radixip_engine.EngineWrapper }

func (a *EngineAdapter) Lookup(ipStr string) bool {
    ip := net.ParseIP(ipStr)
    return ip != nil && a.inner.Lookup(ip) != nil
}
func (a *EngineAdapter) Insert(prefix *net.IPNet, meta radixip_engine.Metadata) error {
    return a.inner.Insert(prefix, meta)
}
func (a *EngineAdapter) Remove(prefix *net.IPNet) *radixip_engine.Metadata {
    return a.inner.Remove(prefix)
}

func main() {
    r := gin.Default()

    engine := &EngineAdapter{inner: radixip_engine.NewEngineWrapper(
        radixip_engine.EngineConcurrent,
        radixip_engine.AtomicRadixNode,
    )}

    // Hot-reload middleware: rate limits and route policy reload from file
    // on change; the blocklist engine state lives outside the config lifecycle.
    mw, stop, err := radixipgin.NewFromYAML("config/radixip.yaml", engine)
    if err != nil {
        log.Fatalf("radixip: %v", err)
    }
    defer stop()

    r.Use(mw)

    r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
    r.POST("/api/v1/auth", func(c *gin.Context) { c.JSON(200, gin.H{"token": "..."}) })
    r.GET("/api/v1/public", func(c *gin.Context) { c.JSON(200, gin.H{"data": "..."}) })

    r.Run(":8080")
}
```

### Echo

```go
import (
    radixipecho "github.com/Mwangi-Derrick/radixip/lib/go/adapters/echo"
    "github.com/labstack/echo/v4"
)

e := echo.New()

mw, stop, err := radixipecho.NewFromYAML("config/radixip.yaml", engine)
if err != nil { log.Fatal(err) }
defer stop()

e.Use(mw)

e.GET("/health", func(c echo.Context) error { return c.String(200, "ok") })
e.POST("/api/v1/auth", func(c echo.Context) error { return c.JSON(200, map[string]any{"token": "..."}) })
e.GET("/api/v1/public", func(c echo.Context) error { return c.JSON(200, map[string]any{"data": "..."}) })

e.Start(":8080")
```

### Fiber

```go
import (
    radixipfiber "github.com/Mwangi-Derrick/radixip/lib/go/adapters/fiber"
    "github.com/gofiber/fiber/v2"
)

app := fiber.New()

mw, stop, err := radixipfiber.NewFromYAML("config/radixip.yaml", engine)
if err != nil { log.Fatal(err) }
defer stop()

app.Use(mw)

app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })
app.Post("/api/v1/auth", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"token": "..."}) })
app.Get("/api/v1/public", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"data": "..."}) })

app.Listen(":8080")
```

### Go gRPC

```go
import (
    "net"
    "log"
    "google.golang.org/grpc"
    radixipgrpc "github.com/Mwangi-Derrick/radixip/lib/go/adapters/grpc-interceptor"
)

func main() {
    unary, stream, stop, err := radixipgrpc.NewFromYAML("config/radixip.yaml", engine)
    if err != nil {
        log.Fatalf("radixip gRPC: %v", err)
    }
    defer stop()

    s := grpc.NewServer(
        grpc.UnaryInterceptor(unary),
        grpc.StreamInterceptor(stream),
    )

    // Register your services
    // pb.RegisterMyServiceServer(s, &myServer{})

    lis, _ := net.Listen("tcp", ":50051")
    log.Println("gRPC listening on :50051")
    s.Serve(lis)
}
```

The Go gRPC hot-reload interceptor uses the **same** `routeTrie` + `autoBan` + `limiter` state as the HTTP adapters. gRPC full-method paths (`/package.Service/Method`) are matched against the route trie; if no route entry matches, the global limiter applies.

---

## Rust Middleware

### Axum

```rust
use axum::{routing::get, Router};
use radixip_axum::AxumWatchedRadixIpLayer;
use radixip_policy::watcher::ConfigWatcher;
use std::{net::SocketAddr, sync::Arc};
use tokio::net::TcpListener;

#[tokio::main]
async fn main() {
    let engine = Arc::new(radixip::new_high_performance().await);
    let watcher = Arc::new(ConfigWatcher::new("config/radixip.yaml").unwrap());

    let app = Router::new()
        .route("/health", get(|| async { "ok" }))
        .route("/api/v1/public", get(|| async { "public ok" }))
        .route("/api/v1/auth", axum::routing::post(|| async { "auth ok" }))
        // Layer is applied to all routes above.
        .layer(AxumWatchedRadixIpLayer::new(watcher, engine));

    let listener = TcpListener::bind("0.0.0.0:8080").await.unwrap();
    println!("Axum listening on :8080");
    axum::serve(listener, app).await.unwrap();
}
```

The `AxumWatchedRadixIpLayer` reads the current `PolicyState` on every request via a wait-free `ArcSwap` pointer load. The state carries the token-bucket limiter, route trie, and auto-ban tracker. Hot-reloads happen in a background thread triggered by `notify`.

### Actix-Web

```rust
use actix_web::{web, App, HttpServer};
use radixip_actix::ActixWatchedRadixIpMiddleware;
use radixip_policy::watcher::ConfigWatcher;
use std::sync::Arc;

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let engine = Arc::new(radixip::new_high_performance().await);
    let watcher = Arc::new(ConfigWatcher::new("config/radixip.yaml").unwrap());

    HttpServer::new(move || {
        App::new()
            .wrap(ActixWatchedRadixIpMiddleware::new(
                watcher.clone(),
                engine.clone(),
            ))
            .route("/health", web::get().to(|| async { "ok" }))
            .route("/api/v1/public", web::get().to(|| async { "public ok" }))
            .route("/api/v1/auth", web::post().to(|| async { "auth ok" }))
    })
    .bind("0.0.0.0:8080")?
    .run()
    .await
}
```

### Tower (generic)

Use `radixip-tower` for any service that uses the Tower `Service` trait — including `hyper`, custom proxies, and non-HTTP services:

```rust
use radixip_tower::from_yaml::TowerWatchedRadixIpLayer;
use radixip_policy::watcher::ConfigWatcher;
use tower::ServiceBuilder;

let watcher = Arc::new(ConfigWatcher::new("config/radixip.yaml").unwrap());
let engine = Arc::new(my_engine);

let service = ServiceBuilder::new()
    .layer(TowerWatchedRadixIpLayer::new(watcher, engine))
    .service(my_inner_service);
```

### Tonic gRPC

```rust
use radixip_grpc_interceptor::from_yaml::{GrpcWatchedRadixIpInterceptor, GrpcWatchedRadixIpLayer};
use radixip_policy::watcher::ConfigWatcher;
use std::sync::Arc;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let engine = Arc::new(radixip::new_high_performance().await);
    let watcher = Arc::new(ConfigWatcher::new("config/radixip.yaml")?);

    let addr = "0.0.0.0:50051".parse()?;

    // Option A — tonic Interceptor trait (metadata-only, cheapest path)
    // Use when you do not need route-trie per-RPC limits.
    let interceptor = GrpcWatchedRadixIpInterceptor::new(watcher.clone(), engine.clone());
    let svc = tonic::service::interceptor(my_grpc_service, interceptor);

    // Option B — Tower Layer (full HTTP/2 request, supports route-trie)
    // Use when you want per-RPC-method rate-limit overrides via rate_limit_routes.
    tonic::transport::Server::builder()
        .layer(GrpcWatchedRadixIpLayer::new(watcher, engine))
        .add_service(my_grpc_service)
        .serve(addr)
        .await?;

    Ok(())
}
```

**Option A vs Option B:** The `Interceptor` trait receives only gRPC metadata (headers), so it cannot inspect the URI path — route-trie matching is skipped and the global rate limiter is always used. Option B (Tower Layer) has access to the full `http::Request`, so gRPC full-method paths like `/radixip.v1.RadixService/Lookup` are matched against `rate_limit_routes` entries. Both options support auto-ban via the `auto_ban` field on `PolicyState`.

---

## Hot-Reloading (Zero Downtime)

Go and Rust adapters watch the YAML file directly with `fsnotify` / `notify`. When the file changes:

1. The new config is parsed in the background thread.
2. A new `PolicyState` (limiter, route trie, auto-ban tracker) is built atomically.
3. The next request reads the new state via a wait-free pointer load — **zero lock contention**, **zero downtime**.

The blocklist engine's tree state is managed separately. Prefix insertions and removals via the engine API persist across hot-reloads.

Node.js and Python load the config once at startup into one process-level `RadixPolicy` object. Runtime config reloads in those ecosystems require restarting the policy object or implementing a custom reload trigger.

---

## Node.js Middleware

```bash
npm install radixip
# Framework packages remain your application's dependency:
npm install express        # or fastify, next, etc.
```

### Express

```js
const express = require('express');
const { radixipExpress } = require('radixip/middleware');

const app = express();

// Tell Express which proxy addresses to trust so req.ip is resolved correctly.
app.set('trust proxy', ['loopback', '10.0.0.0/8']);

app.use(radixipExpress({
  configPath: 'config/radixip.yaml',
}));

app.get('/', (_req, res) => res.json({ ok: true }));
app.listen(3000);
```

Pass a pre-created `policy` when you want to share the same instance with other parts of the application:

```js
const { RadixPolicy } = require('radixip');
const { radixipExpress } = require('radixip/middleware');

const policy = RadixPolicy.fromYaml('config/radixip.yaml');
app.use(radixipExpress({ policy }));
```

### Next.js

```ts
// middleware.ts  (project root — applies before every page and API route)
import { radixipNext } from 'radixip/middleware';

export default radixipNext({
  configPath: 'config/radixip.yaml',
  // Provide resolveIp when behind a trusted proxy:
  // resolveIp: (req) => req.headers.get('x-real-ip'),
});

export const config = {
  matcher: ['/((?!_next/static|_next/image|favicon.ico).*)'],
};
```

> **Edge Runtime:** The `radixip` native addon is a compiled Node.js module. It cannot run in the Next.js Edge Runtime. Set `export const runtime = 'nodejs'` on the middleware file or use a Node-hosted deployment target.

### TanStack Start

TanStack Start uses the same Web `Request`/`Response` model as Next.js:

```ts
// app/middleware.ts
import { radixipTanStackStart } from 'radixip/middleware';

export const radixipGate = radixipTanStackStart({
  configPath: 'config/radixip.yaml',
  resolveIp: (request) => request.headers.get('x-real-ip'),
});
```

Register `radixipGate` through the TanStack Start request middleware hook for your Start version. Keep the policy at module scope — never construct `RadixPolicy.fromYaml(...)` inside the request handler.

### Fastify

Two usage styles are available:

**Hook style** — direct `preHandler` registration:

```js
const fastify = require('fastify')();
const { radixipFastify } = require('radixip/middleware');

fastify.addHook('preHandler', radixipFastify({
  configPath: 'config/radixip.yaml',
}));

fastify.get('/', async () => ({ ok: true }));
fastify.listen({ port: 3000 });
```

**Plugin style** — registers the hook globally and decorates the instance with `fastify.radixip.check(ip)` for manual use on specific routes:

```js
const { radixipFastifyPlugin } = require('radixip/middleware');

fastify.register(radixipFastifyPlugin, {
  configPath: 'config/radixip.yaml',
  global: true,  // default; set false to only use fastify.radixip.check manually
});

// Manual check on a specific route:
fastify.get('/admin', {
  preHandler: async (request, reply) => {
    const result = fastify.radixip.check(request.ip);
    if (result.decision !== 'allow') {
      return reply.status(403).send({ error: 'blocked' });
    }
  },
}, async () => ({ admin: true }));
```

---

## Python Middleware

```bash
pip install radixip
# Framework packages remain your application's dependency:
pip install fastapi uvicorn   # or flask, django, etc.
```

### FastAPI / Starlette

```python
from fastapi import FastAPI
from radixip import RadixPolicy
from radixip.middleware import RadixIPMiddleware

app = FastAPI()

# Create once at startup — the policy object is expensive to build.
policy = RadixPolicy.from_yaml("config/radixip.yaml")

app.add_middleware(RadixIPMiddleware, policy=policy)

@app.get("/")
async def root():
    return {"ok": True}
```

The default IP extractor reads `request.client.host` (the direct peer address). For deployments behind a trusted ingress proxy, supply a `resolve_ip` function:

```python
def resolve_ip(request):
    # Only after the ingress proxy has been authenticated and re-written the header.
    xff = request.headers.get("x-forwarded-for", "")
    return xff.split(",")[0].strip() or None

app.add_middleware(RadixIPMiddleware, policy=policy, resolve_ip=resolve_ip)
```

`RadixIPMiddleware` also works with plain Starlette — add it with `Starlette(middleware=[...])` using the same `RadixIPMiddleware` class.

### Flask

```python
from flask import Flask
from radixip import RadixPolicy
from radixip.middleware import make_flask_hook

app = Flask(__name__)
policy = RadixPolicy.from_yaml("config/radixip.yaml")

# Runs before every request. Returning a Response short-circuits the handler.
app.before_request(make_flask_hook(policy))

@app.get("/")
def index():
    return {"ok": True}
```

Custom IP resolver for Flask (e.g. behind nginx):

```python
def resolve_ip(request):
    return request.headers.get("X-Real-IP") or request.remote_addr

app.before_request(make_flask_hook(policy, resolve_ip=resolve_ip))
```

### Django

Add `RadixIPDjangoMiddleware` early in `settings.MIDDLEWARE` and point it at a pre-built policy:

```python
# settings.py
from radixip import RadixPolicy

RADIXIP_POLICY = RadixPolicy.from_yaml("config/radixip.yaml")
# Optional: RADIXIP_RESOLVE_IP = lambda request: request.META.get("HTTP_X_REAL_IP")

MIDDLEWARE = [
    "radixip.middleware.RadixIPDjangoMiddleware",   # must come first
    "django.middleware.security.SecurityMiddleware",
    # ...
]
```

Alternatively, pass the config path and let the middleware load it once:

```python
RADIXIP_CONFIG_PATH = "config/radixip.yaml"
```

### Low-level: direct policy call

Every built-in adapter calls the same two methods. For any framework not covered above:

```python
result = policy.check_ip(client_ip)   # returns dict

decision = result["decision"]          # "allow" | "block" | "auto_ban" | "limit" | "bad_request"
retry_after = result["retry_after_seconds"]  # int, relevant when decision == "limit"

if decision == "allow":
    pass  # continue
elif decision == "limit":
    return {"error": "rate limited"}, 429, {"Retry-After": str(retry_after or 1)}
elif decision in ("block", "auto_ban"):
    return {"error": "blocked"}, 403
else:
    return {"error": "invalid client IP"}, 400
```

---

## Native Policy and FFI Boundary

The Rust policy engine owns all shared state:

- Radix-tree blocklist lookups (~60 ns)
- Token-bucket maps and atomic updates (~200 ns)
- Route-trie path matching
- Violation sliding windows and auto-ban injection
- Configuration validation and hot-swap

Python and Node call the native binding with one IP string and receive a small decision object. The request path must not cross the FFI boundary with a YAML parse, a JSON decode, or a newly allocated policy object — create the `RadixPolicy` once at startup and reuse it for every request.

C and C++ can use the packed-IP policy ABI directly from `lib/rust/ffi/radixip_policy.h`.

---

## 🐳 Docker Sidecar Deployment

RadixIP can be deployed as an independent, high-performance sidecar service.

```bash
docker run -d \
  --name radixip-sidecar \
  -p 50051:50051 \
  -p 9090:9090 \
  -v $(pwd)/radixip.yaml:/etc/radixip/radixip.yaml:ro \
  ghcr.io/mwangi-derrick/radixip/sidecar:latest
```

Any modification to the mounted `radixip.yaml` is **automatically hot-reloaded** by the background watcher without restarting the container.
