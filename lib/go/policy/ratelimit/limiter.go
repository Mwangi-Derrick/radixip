package ratelimit

import "time"

// Limiter is the common runtime-dispatch surface used by policy configuration.
type Limiter interface {
	Allow(key string) bool
}

type fixedWindowAdapter struct{ limiter *FixedWindowCounter }

func (a fixedWindowAdapter) Allow(key string) bool { return a.limiter.Allow(key, time.Now()) }

type slidingCounterAdapter struct{ limiter *SlidingWindowCounter }

func (a slidingCounterAdapter) Allow(key string) bool { return a.limiter.Allow(key, time.Now()) }

type slidingLogAdapter struct{ limiter *SlidingWindowLog }

func (a slidingLogAdapter) Allow(key string) bool { return a.limiter.Allow(key, time.Now()) }

// NewLimiter constructs the configured algorithm. Unknown names fall back to
// token_bucket to preserve behavior for older configurations.
func NewLimiter(algorithm string, capacity, rate uint64, window time.Duration, ttlSecs uint32, maxBuckets uint64) Limiter {
	switch algorithm {
	case "leaky_bucket":
		return NewLeakyBucketLimiter(capacity, rate, ttlSecs, maxBuckets)
	case "fixed_window_counter":
		return fixedWindowAdapter{NewFixedWindowCounter(int(min(capacity, uint64(^uint(0)>>1))), window)}
	case "sliding_window_log":
		return slidingLogAdapter{NewSlidingWindowLog(int(min(capacity, uint64(^uint(0)>>1))), window)}
	case "sliding_window_counter":
		return slidingCounterAdapter{NewSlidingWindowCounter(int(min(capacity, uint64(^uint(0)>>1))), window)}
	default:
		return NewTokenBucketLimiter(capacity, rate, ttlSecs, maxBuckets)
	}
}
