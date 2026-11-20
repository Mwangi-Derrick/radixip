# radixip-tower

Framework-neutral Tower middleware for RadixIP IP blocklists, rate limiting, per-route policies, and auto-banning.

## Install

```toml
[dependencies]
radixip-tower = "0.1"
```

## Hot-reloaded layer

```rust
use std::sync::Arc;
use radixip_policy::ConfigWatcher;
use radixip_tower::TowerWatchedRadixIpLayer;
use tower::ServiceBuilder;

let engine = Arc::new(radix_engine); // Arc<Box<dyn RadixEngine>>
let watcher = Arc::new(ConfigWatcher::new_with_engine(
    "radixip.yaml",
    Some(engine.clone()),
)?);

let service = ServiceBuilder::new()
    .layer(TowerWatchedRadixIpLayer::new(watcher, engine))
    .service(inner_service);
```

The watched layer supports HTTP request paths and methods for route-specific limits. It extracts `x-forwarded-for`, `x-real-ip`, or a `SocketAddr` request extension, then applies blocklist, rate-limit, and auto-ban policy before forwarding the request.

For a non-watching policy, use `RadixIpLayer` with an `Arc<PolicyEngine>` and `ResponseConfig`.

## Configuration

The watcher loads the shared RadixIP YAML schema. The
[runnable example](../../../../config/examples/rate-limit-algorithms.yaml)
shows global defaults and all five limiter choices. `algorithm` selects the
policy, `capacity` sets the allowance, and `window_seconds` configures fixed
and sliding windows. See the [rate-limit field guide](../../../../docs/rate-limiting.md)
for semantics and the [middleware guide](../../../../docs/middleware.md) for
route matching, trusted proxies, auto-ban, and response settings.
For other shared sections, see the [configuration guide](../../../../docs/configuration.md).

## License

MIT
