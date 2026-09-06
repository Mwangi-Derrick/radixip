# radixip-axum

Axum middleware for RadixIP IP blocklists, rate limiting, per-route policies, and auto-banning.

## Install

```toml
[dependencies]
radixip-axum = "0.1"
```

## Hot-reloaded configuration

Use `AxumWatchedRadixIpLayer` when policy settings should be reloaded from YAML without restarting the server. The blocklist engine remains shared with the application.

```rust
use std::sync::Arc;
use axum::{routing::get, Router};
use radixip_axum::AxumWatchedRadixIpLayer;
use radixip_policy::ConfigWatcher;

let engine = Arc::new(radix_engine); // Arc<Box<dyn RadixEngine>>
let watcher = Arc::new(ConfigWatcher::new_with_engine(
    "radixip.yaml",
    Some(engine.clone()),
)?);

let app = Router::new()
    .route("/health", get(|| async { "ok" }))
    .layer(AxumWatchedRadixIpLayer::new(watcher, engine));
```

The layer extracts `x-forwarded-for`, `x-real-ip`, or the peer address, checks the blocklist and rate limits, and returns configured HTTP responses for blocked or limited requests.

For a non-watching policy, use the re-exported `RadixIpLayer` with an `Arc<PolicyEngine>`.

## Configuration

The adapter uses the shared RadixIP YAML schema. See the [middleware guide](../../../../docs/middleware.md) for `rate_limit`, `rate_limit_routes`, `auto_ban`, trusted proxies, and response settings.

## License

MIT
