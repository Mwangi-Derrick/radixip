# Rate limiting configuration

Go and Rust read YAML through their config packages. Node.js and Python use
the same Rust policy loader through the native bindings. The shared runnable
example is [`config/examples/rate-limit-algorithms.yaml`](../config/examples/rate-limit-algorithms.yaml).
It contains one route for each supported algorithm; use the route that matches
your application or copy its `rate_limit` block into the global section.

## Algorithm selection

Set `algorithm` to one of the following values:

| Value | Behavior | Additional setting |
|---|---|---|
| `token_bucket` | Allows a burst up to `capacity`, then replenishes continuously at `refill_rate` requests per second. | `refill_rate` |
| `leaky_bucket` | Allows a burst up to `capacity`; available headroom returns at `refill_rate` requests per second. Excess requests are rejected, not queued. | `refill_rate` |
| `fixed_window_counter` | Counts requests in aligned, fixed length windows. A client can use its full allowance on both sides of a window boundary. | `window_seconds` |
| `sliding_window_log` | Keeps each accepted request timestamp and enforces an exact rolling window. It uses more memory per active client than the counter algorithms. | `window_seconds` |
| `sliding_window_counter` | Estimates the rolling count by weighting the previous fixed window and adding the current count. It is compact but approximate. | `window_seconds`; `capacity` is at most 65535 |

The fixed and sliding counters store their state in packed atomic words. The
sliding log retains exact timestamps and synchronizes updates per client key.

## Fields

Every global or route-level `rate_limit` block accepts these fields:

| Field | Meaning |
|---|---|
| `enabled` | Turns that limiter on. A route limiter overrides the global limiter when the route and method match. |
| `algorithm` | Chooses one of the five names above. Defaults to `token_bucket`. |
| `capacity` | Token/leaky bucket burst capacity, or the maximum request count in a window for the counter/log algorithms. Must be positive when enabled. |
| `refill_rate` | Token refill or leaky headroom recovery per second. Required by token and leaky buckets; unused by the window algorithms. |
| `window_seconds` | Window length for fixed counter, sliding log, and sliding counter. It is ignored by token and leaky buckets. |
| `bucket_mode` | `ip` creates a bucket per client IP; `subnet` groups by the configured IPv4/IPv6 prefix depth; `both` checks both. |
| `max_buckets` | Maximum tracked keys in the bounded Rust policy cache. Go's current in-memory limiter maps do not evict at this limit. |
| `ttl_seconds` | Idle cache lifetime in Rust; Go token/leaky buckets use lazy idle reset. Current Go counter and log state is retained until process restart. |

For subnet and both modes, set `depth_v4` and `depth_v6` under `bucket_mode`.
The defaults are `/24` and `/48`. Only trust forwarded IP headers from proxies
that are listed in `middleware.trusted_proxies`; otherwise a client may choose
its own rate-limit key.

## Route policies

`rate_limit_routes.enabled` enables method/path-specific overrides. Each entry
has a `path`, a list of `methods`, and its own `rate_limit` block. Route
configuration uses the same algorithm names and fields as the global policy.
If no route matches, the global rate limiter applies.

The complete example covers all five algorithms and can be loaded directly by
the Go and Rust YAML watchers, Node's `RadixPolicy.fromYaml`, and Python's
`RadixPolicy.from_yaml`.
