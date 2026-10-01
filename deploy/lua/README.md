# deploy/lua — Redis Lua Rate Limiting Scripts

This directory contains Redis Lua scripts for **distributed, atomic rate limiting**.
Each script is loaded once via `SCRIPT LOAD` and thereafter invoked by its SHA1
fingerprint using `EVALSHA`, which is faster than `EVAL` and avoids retransmitting
the full script body on every call.

---

## Why Lua on Redis?

Redis executes each Lua script **atomically** — the entire script body runs as a
single logical transaction. No other Redis command can interleave between any two
`redis.call()` invocations inside the script. This gives us:

- **No race conditions** — read-modify-write sequences (load tokens → compute refill
  → deduct → store) are safe without `WATCH`/`MULTI`/`EXEC`.
- **No round-trip penalty** — all Redis commands within one script are pipelined
  inside the Redis process; only one network RTT is paid by the caller.
- **Determinism** — Redis can replicate Lua scripts to replicas and AOF logs by
  recording the script's effects, not re-running the script.

---

## EVAL vs EVALSHA

| Command  | Description |
|----------|-------------|
| `EVAL script numkeys key... arg...` | Sends the full Lua source on every call. Convenient for development. |
| `EVALSHA sha1 numkeys key... arg...` | Sends only the 40-byte SHA1 of a previously loaded script. |

**Workflow:**

```
# Once at startup (or when the script changes):
SHA=$(redis-cli SCRIPT LOAD "$(cat token_bucket.lua)")
echo $SHA   # e.g. a1b2c3...

# On every rate-limit check:
redis-cli EVALSHA $SHA 1 "rl:tb:ip:10.0.0.1" 100 10 1700000000 1
```

In Go (using `go-redis/v9`):

```go
sha, err := rdb.ScriptLoad(ctx, tokenBucketScript).Result()
// store sha, reuse across all calls

res, err := rdb.EvalSha(ctx, sha,
    []string{"rl:tb:ip:10.0.0.1"},   // KEYS
    100, 10.0, now.Unix(), 1,         // ARGV
).Int64Slice()
allowed, remainingX1000 := res[0], res[1]
```

> **NOSCRIPT error**: if Redis restarts and loses its script cache, `EVALSHA` returns
> `NOSCRIPT`. The Go client should catch this error, reload with `SCRIPT LOAD`, and
> retry. The `go-redis` `Script` helper (`redis.NewScript(src)`) does this automatically.

---

## Key Naming Convention

```
rl:{algo}:{id_type}:{id}
```

| Segment    | Values                               | Example                    |
|------------|--------------------------------------|----------------------------|
| `rl`       | constant namespace                   | `rl`                       |
| `{algo}`   | `tb` token bucket · `lb` leaky · `sw` sliding window | `tb`      |
| `{id_type}`| `ip` · `route` · `user` · `apikey`  | `ip`                       |
| `{id}`     | concrete identifier value            | `192.0.2.42`               |

**Full example:** `rl:tb:ip:192.0.2.42`

This convention enables:
- Pattern-based scanning: `SCAN 0 MATCH rl:tb:ip:* COUNT 100`
- Per-algorithm Redis keyspace notifications
- Easy isolation in Redis Cluster (all keys for one IP hash to the same slot
  when using hash tags: `rl:tb:{ip:192.0.2.42}`)

---

## Performance Notes

| Approach | Latency | Accuracy | Use When |
|---|---|---|---|
| **Local CAS** (in-process atomic) | ~5–50 ns | Per-pod only; no global view | Single-pod services; ultra-low latency path |
| **Redis Lua** (token bucket) | ~0.2–0.5 ms network RTT | Globally consistent across all pods | Multi-pod deployments; per-IP or per-user limits |
| **Redis Lua** (sliding window) | ~0.3–0.8 ms | Exact, no approximation | Low-volume API quota enforcement |

**Rule of thumb:**
- For **per-pod** self-protection (e.g., CPU overload guard): use the in-process
  token bucket in `ratelimit/local.go` — nanosecond overhead, zero network cost.
- For **global** rate limits shared across all replicas (e.g., per-client-IP at the
  ingress layer): use Redis Lua scripts — pay the ~0.3 ms RTT, gain consistency.

The sidecar proxy always prefers the local limiter first; the Redis limiter is only
consulted when the local bucket allows the request (two-tier admission).

---

## Failover Strategy

When Redis is **unreachable** (connection timeout, sentinel failover in progress, etc.),
the gRPC sidecar falls back to the **in-process token bucket** to avoid turning a
Redis outage into a service outage.

Failover logic (in `ratelimit/redis_limiter.go`):

```
1. Attempt EVALSHA against Redis with a 5 ms deadline.
2. On success → use the Redis decision (allowed / denied).
3. On NOSCRIPT → reload script and retry once.
4. On any other error (timeout, CLUSTERDOWN, READONLY, etc.):
     → increment redis_unavailable_total Prometheus counter
     → delegate to in-process TokenBucket.Allow()
     → emit a WARN log entry with the error
5. Circuit breaker: after 5 consecutive Redis failures,
   open the circuit for 10 seconds before retrying.
```

This ensures **fail-open** behavior: when Redis is down, requests are still
rate-limited locally per-pod rather than being universally allowed or universally
denied.

---

## Script Inventory & Phase Roadmap

| File | Algorithm | Status | Notes |
|---|---|---|---|
| [`token_bucket.lua`](token_bucket.lua) | Token Bucket | ✅ **PHASE-1 — production** | O(1) space; supports bursting |
| [`leaky_bucket.lua`](leaky_bucket.lua) | Leaky Bucket (policer) | 🚧 **PHASE-2 — stub** | O(1) space; smooth rate, no bursting |
| [`sliding_window.lua`](sliding_window.lua) | Sliding Window Log | 📋 **PHASE-3 — stub** | O(limit) space; exact; low-volume only |

### Phase-1 (current)
Token bucket is fully implemented and unit-tested. All production traffic uses
`token_bucket.lua` via `EVALSHA`.

### Phase-2 targets
- Leaky bucket implementation for downstream service protection where burst
  traffic is explicitly unwanted (e.g., database connection rate limiting).
- Target: next sprint after service mesh integration.

### Phase-3 targets  
- Sliding window log for authenticated user API quotas (e.g., 1000 req/hour
  per API key) where exactness matters more than throughput.
- Evaluate the sliding window *counter* approximation as a memory-efficient
  alternative before committing to the full log variant.
