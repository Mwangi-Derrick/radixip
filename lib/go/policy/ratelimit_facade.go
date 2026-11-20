package policy

import (
	"time"

	"github.com/Mwangi-Derrick/radixip/lib/go/config"
	"github.com/Mwangi-Derrick/radixip/lib/go/policy/ratelimit"
)

// RateLimiter is the common interface for configured limiter algorithms.
type RateLimiter = ratelimit.Limiter

// TokenBucketLimiter preserves the original public type for callers that
// construct token buckets directly.
type TokenBucketLimiter = ratelimit.TokenBucketLimiter

func NewTokenBucketLimiter(capacity, refillRate uint64, ttlSecs uint32, maxBuckets uint64) *TokenBucketLimiter {
	return ratelimit.NewTokenBucketLimiter(capacity, refillRate, ttlSecs, maxBuckets)
}

// NewConfiguredRateLimiter builds the algorithm selected in configuration.
func NewConfiguredRateLimiter(cfg config.RateLimitConfig) RateLimiter {
	return ratelimit.NewLimiter(cfg.Algorithm, cfg.Capacity, cfg.RefillRate,
		time.Duration(cfg.WindowSeconds)*time.Second, cfg.TTLSeconds, cfg.MaxBuckets)
}
