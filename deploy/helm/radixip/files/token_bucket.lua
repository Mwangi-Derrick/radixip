-- token_bucket.lua
-- Atomic token bucket rate limiter for Redis.
--
-- Executed with EVALSHA so the compiled script body is cached by its SHA1
-- fingerprint. Redis guarantees that the entire script runs atomically — no
-- other command can interleave — so we never need WATCH/MULTI/EXEC.
--
-- Usage (from Go via go-redis):
--   sha, _ := client.ScriptLoad(ctx, script).Result()
--   res, _ := client.EvalSha(ctx, sha,
--       []string{"rl:tb:ip:192.0.2.1"},          -- KEYS
--       capacity, refill_rate, now_unix, cost,    -- ARGV
--   ).Result()
--
-- Key naming convention: rl:{algo}:{id_type}:{id}
--   rl        = rate-limit namespace
--   tb        = token bucket algorithm
--   ip|route  = identifier type
--   <value>   = the concrete identifier
--
-- Returns: {allowed, remaining_tokens_x1000}
--   allowed             : 1 if the request is permitted, 0 if rejected
--   remaining_tokens_x1000 : floor(remaining * 1000) — integer proxy for a
--                            float, preserving 3 decimal places of precision
--                            without requiring RESP3 double type support.

-- ---------------------------------------------------------------------------
-- Input validation
-- ---------------------------------------------------------------------------

-- KEYS[1]: the Redis hash key for this bucket, e.g. "rl:tb:ip:10.0.0.1"
local bucket_key = KEYS[1]

-- ARGV[1]: maximum token capacity (integer, e.g. 100)
local capacity    = tonumber(ARGV[1])
-- ARGV[2]: refill rate in tokens per second (float, e.g. 10.0)
local refill_rate = tonumber(ARGV[2])
-- ARGV[3]: current unix timestamp in seconds (integer or float)
local now         = tonumber(ARGV[3])
-- ARGV[4]: cost of this request in tokens (usually 1)
local cost        = tonumber(ARGV[4])

-- Guard against malformed arguments. Return denied + 0 remaining so the
-- caller can distinguish a config error from a legitimate rejection.
local ok, err = pcall(function()
    assert(capacity    ~= nil and capacity    > 0, "capacity must be > 0")
    assert(refill_rate ~= nil and refill_rate > 0, "refill_rate must be > 0")
    assert(now         ~= nil,                     "now timestamp is required")
    assert(cost        ~= nil and cost        > 0, "cost must be > 0")
end)
if not ok then
    -- Log to Redis slow log visible in MONITOR; surface the error string.
    redis.log(redis.LOG_WARNING, "token_bucket: bad args: " .. tostring(err))
    return {0, 0}
end

-- ---------------------------------------------------------------------------
-- Load existing bucket state from the Redis hash
-- ---------------------------------------------------------------------------
-- HGETALL returns a flat array: {field1, val1, field2, val2, ...}
-- We use HGET for simplicity; two round-trips are collapsed into one pipeline
-- by Redis since we are inside a Lua script (all redis.call() are pipelined
-- automatically within the atomic execution).

local raw_tokens = redis.call("HGET", bucket_key, "tokens")
local raw_ts     = redis.call("HGET", bucket_key, "ts")

-- On first access the key doesn't exist: seed to full capacity.
local tokens = capacity   -- default: full bucket
local ts     = now        -- default: now

if raw_tokens ~= false then
    tokens = tonumber(raw_tokens)
end
if raw_ts ~= false then
    ts = tonumber(raw_ts)
end

-- ---------------------------------------------------------------------------
-- Token refill calculation
-- ---------------------------------------------------------------------------

-- Elapsed time since the bucket was last touched (clamped to >= 0 to handle
-- minor clock skew between Redis replicas if ever used).
local elapsed = math.max(0, now - ts)

-- Tokens earned during the elapsed interval.
local earned = elapsed * refill_rate

-- Refilled amount, capped at capacity (bucket cannot overflow).
local refilled = math.min(capacity, tokens + earned)

-- ---------------------------------------------------------------------------
-- Admission decision
-- ---------------------------------------------------------------------------

local allowed = 0
local remaining = refilled  -- pessimistic: assume rejection

if refilled >= cost then
    -- Sufficient tokens: consume 'cost' tokens and allow the request.
    allowed   = 1
    remaining = refilled - cost

    -- Persist the updated state back to the hash.
    -- HSET field-value pairs set atomically in a single command.
    redis.call("HSET", bucket_key,
        "tokens", tostring(remaining),
        "ts",     tostring(now))
else
    -- Insufficient tokens: persist the refilled (but not consumed) state
    -- so the clock keeps advancing correctly for future requests.
    redis.call("HSET", bucket_key,
        "tokens", tostring(refilled),
        "ts",     tostring(now))
end

-- ---------------------------------------------------------------------------
-- TTL management — auto-expire idle buckets
-- ---------------------------------------------------------------------------
-- We set the expiry to 2× the time it would take to fully refill an empty
-- bucket. An idle key beyond that duration would have been naturally full
-- anyway, so there is no information loss in evicting it.
--
-- ttl_seconds = (capacity / refill_rate) * 2
-- e.g. capacity=100, rate=10 → TTL = 20 seconds

local ttl_seconds = math.ceil((capacity / refill_rate) * 2)
redis.call("EXPIRE", bucket_key, ttl_seconds)

-- ---------------------------------------------------------------------------
-- Return value
-- ---------------------------------------------------------------------------
-- remaining is a Lua float. We multiply by 1000 and floor it to return an
-- integer, giving the caller millisecond-precision without needing RESP3.
-- Callers divide by 1000.0 to recover the float.
--
-- Example: 7.342 tokens remaining → 7342

return {allowed, math.floor(remaining * 1000)}
