# RadixIP Go policy engine

The policy package combines client IP extraction, blocklist decisions,
rate-limit dispatch, route policies, and auto-ban tracking. Configured
limiters implement a common `Allow(key)` interface; `NewConfiguredRateLimiter`
selects the algorithm from `config.RateLimitConfig`.

The [shared YAML example](../../../config/examples/rate-limit-algorithms.yaml)
contains one route per supported limiter. The [field guide](../../../docs/rate-limiting.md)
explains how `capacity`, `refill_rate`, and `window_seconds` map to each
algorithm, plus keying and state-retention behavior.
The [full configuration guide](../../../docs/configuration.md) explains the
other top-level YAML sections.
