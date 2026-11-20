# radixip-grpc-interceptor

Tonic gRPC protection for RadixIP. Provides blocklist checks, global or route-specific rate limiting, auto-ban enforcement, retry metadata, and YAML hot reload.

## Install

```toml
[dependencies]
radixip-grpc-interceptor = "0.1"
```

## Choose one integration path

The tonic `Interceptor` and Tower `Layer` are alternatives. Install one for a server; installing both would evaluate the same RPC twice and double-count rate-limit violations.

### Tonic interceptor

Use this metadata-only path when you need the lowest-overhead unary or streaming interceptor:

```rust
use std::sync::Arc;
use radixip_grpc_interceptor::GrpcWatchedRadixIpInterceptor;
use radixip_policy::ConfigWatcher;
use tonic::service::interceptor;

let engine = Arc::new(radix_engine); // Arc<Box<dyn RadixEngine>>
let watcher = Arc::new(ConfigWatcher::new_with_engine(
    "radixip.yaml",
    Some(engine.clone()),
)?);
let policy_interceptor = GrpcWatchedRadixIpInterceptor::new(watcher, engine);
let service = interceptor(my_grpc_service, policy_interceptor);
```

### Tower layer

Use `GrpcWatchedRadixIpLayer` when composing with tonic's full HTTP/2 request stack:

```rust
use radixip_grpc_interceptor::GrpcWatchedRadixIpLayer;

let server = tonic::transport::Server::builder()
    .layer(GrpcWatchedRadixIpLayer::new(watcher, engine))
    .add_service(my_grpc_service)
    .serve(addr);
```

The Tower layer can inspect the RPC path (`/package.Service/Method`) and applies the current watcher state, including auto-ban state rebuilt with the shared engine.

> The exact tonic service wrapper for an interceptor depends on the service shape. For Tower integration, prefer the layer when both metadata and request URI access are needed.

## gRPC metadata and status mapping

Clients can provide `x-forwarded-for` or `x-real-ip` metadata. Rate-limited calls return `ResourceExhausted`; blocklisted or auto-banned calls return `PermissionDenied`; malformed client address data returns `InvalidArgument`. Rate-limited responses include `retry-after: 1` metadata.

## Configuration

The watcher loads the shared RadixIP YAML schema. The
[runnable example](../../../../config/examples/rate-limit-algorithms.yaml)
shows global defaults and all five limiter choices. `algorithm` selects the
policy, `capacity` sets the allowance, and `window_seconds` configures fixed
and sliding windows. See the [rate-limit field guide](../../../../docs/rate-limiting.md)
for semantics and the [middleware guide](../../../../docs/middleware.md) for
trusted proxies, route policies, auto-ban, and responses.
For other shared sections, see the [configuration guide](../../../../docs/configuration.md).

## License

MIT
