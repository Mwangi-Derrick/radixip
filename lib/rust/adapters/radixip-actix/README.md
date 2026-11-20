# radixip-actix

Actix Web middleware for RadixIP IP blocklists, rate limiting, per-route policies, and auto-banning.

## Install

```toml
[dependencies]
radixip-actix = "0.1"
```

## Hot-reloaded middleware

```rust
use std::sync::Arc;
use actix_web::{web, App, HttpServer};
use radixip_actix::ActixWatchedRadixIpMiddleware;
use radixip_policy::ConfigWatcher;

let engine = Arc::new(radix_engine); // Arc<Box<dyn RadixEngine>>
let watcher = Arc::new(ConfigWatcher::new_with_engine(
    "radixip.yaml",
    Some(engine.clone()),
)?);

HttpServer::new(move || {
    App::new()
        .wrap(ActixWatchedRadixIpMiddleware::new(
            watcher.clone(),
            engine.clone(),
        ))
        .route("/health", web::get().to(|| async { "ok" }))
})
.bind(("0.0.0.0", 8080))?
.run()
.await?;
```

The middleware evaluates the shared policy before the handler, extracts client IP metadata, and returns configured blocked, rate-limited, or bad-request responses. Configuration changes are picked up by the background watcher.

For a non-watching policy, use `RadixIpMiddleware` with an `Arc<PolicyEngine>` and `ResponseConfig`.

## Configuration

The watcher loads the shared RadixIP YAML schema. The
[runnable example](../../../../config/examples/rate-limit-algorithms.yaml)
shows global defaults and all five limiter choices. `algorithm` selects the
policy, `capacity` sets the allowance, and `window_seconds` configures fixed
and sliding windows. See the [rate-limit field guide](../../../../docs/rate-limiting.md)
for semantics and the [middleware guide](../../../../docs/middleware.md) for
trusted proxies, route limits, auto-ban, and responses.
For other shared sections, see the [configuration guide](../../../../docs/configuration.md).

## License

MIT
