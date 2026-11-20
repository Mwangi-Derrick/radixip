# RadixIP configuration

The canonical configuration format is YAML. Go and Rust load it directly;
Node.js and Python load it through the native Rust policy binding. TOML parsing
is not implemented in these loaders; [`config/radixip.toml`](../config/radixip.toml)
is a reference-only equivalent. See [`config/radixip.yaml`](../config/radixip.yaml)
for the full deployment example and [`config/examples/rate-limit-algorithms.yaml`](../config/examples/rate-limit-algorithms.yaml)
for route examples using all five limiter algorithms.

## Top-level sections

All fields are nested below `radixip:`.

| Section | Purpose |
|---|---|
| `engine` | Selects the radix tree implementation, node layout, shard count, and lookup cache. |
| `middleware` | Chooses the client-IP source, trusted proxy ranges, and denial response codes. |
| `blocklist` | Enables static blocked-prefix data loaded from inline entries or a file. |
| `xdp` | Configures the optional kernel-level blocklist mirror and its Redis feed. |
| `rate_limit` | Selects the global limiter algorithm and per-client key policy. |
| `rate_limit_routes` | Adds method/path-specific limiter overrides. |
| `auto_ban` | Temporarily adds repeat rate-limit offenders to the blocklist engine. |
| `metrics` | Controls Prometheus metrics exposure. |

## Engine

```yaml
engine:
  variant: "concurrent"
  node_variant: "atomic"
  num_shards: 16
  cache:
    enabled: true
    max_entries: 10000
    ttl_seconds: 3600
```

`variant` selects the engine implementation (`concurrent`, `standard`,
`lock_free`, or `adaptive`, subject to the engine package). `node_variant`
selects the node representation. `num_shards` controls sharding for variants
that use it. `cache.enabled` toggles the lookup-result cache;
`max_entries` bounds its entries and `ttl_seconds` sets their lifetime.

## Middleware and client IP

`ip_source` accepts `x-forwarded-for`, `x-real-ip`, or `remote-addr`.
`trusted_proxies` is a list of CIDRs whose forwarded client-IP headers may be
used. Keep this list limited to proxies that overwrite forwarding headers.
`responses.blocked` and `responses.rate_limited` set the HTTP response status
codes for blocked and throttled clients (defaults: 403 and 429).

## Blocklist

Set `blocklist.enabled` to activate static source loading. Each item in
`blocklist.sources` has `type: inline` with a `subnets` list, or `type: file`
with a `path`. Engine inserts made at runtime are separate from these static
sources.

## XDP mirror

`xdp.enabled` turns on the optional kernel mirror. `interface` names the
network device; `mode` selects `skb`, `eth0`, or `auto`; `pin_dir` sets the BPF
pin location; and `fail_closed` chooses whether XDP errors fail closed.
`min_v4_prefix` and `min_v6_prefix` set minimum prefix lengths mirrored to the
kernel. `never_block` lists protected addresses or prefixes. `redis.url`,
`channel`, and `snapshot_key` select the Redis feed and snapshot used by the
mirror.

## Rate limiting

`rate_limit.enabled` switches on the global policy. `algorithm` selects the
limiter, `capacity` sets its burst or request allowance, and `refill_rate` or
`window_seconds` supplies the algorithm-specific rate/window. `max_buckets` and
`ttl_seconds` configure per-client state bounds/lifetime where supported.
`bucket_mode.mode` accepts `ip`, `subnet`, or `both`; subnet modes use
`depth_v4` and `depth_v6` to choose prefix lengths.

For the exact behavior and limits of each algorithm, see the
[rate-limiting guide](rate-limiting.md).

## Route policies

Set `rate_limit_routes.enabled` to enable overrides. Each item in `routes`
contains a `path`, a list of HTTP `methods`, and a complete `rate_limit` block.
An empty method list applies to all methods where supported. Route limiters use
the same algorithm names and settings as the global limiter; if no route
matches, the global policy applies.

## Auto-ban and metrics

When `auto_ban.enabled` is true, `threshold_violations` sets the number of
rate-limit violations required for a ban, `window_seconds` is the observation
window, and `ban_duration_seconds` sets the ban length.

`metrics.enabled` toggles metrics collection/exposure and `prometheus_path`
sets the endpoint path (default `/metrics`).
