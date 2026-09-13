// Package policy implements the per-IP Leaky Bucket rate limiter for RadixIP.
//
// The bucket state is stored in a 256-shard map. Each entry is a single uint64
// bit-packed as:
//
//	63          32 31            0
//	┌─────────────┬──────────────┐
//	│  unix secs  │ headroom×1000│
//	│  (32 bits)  │  (32 bits)   │
//	└─────────────┴──────────────┘
//
// Semantics: the packed field stores *headroom* = (capacity - water) in
// fixed-point ×1000. Water (pending drops) leaks out at leakRate per second,
// which increases headroom. A request is admitted if there is at least 1.0
// drop of headroom, and consumes 1.0 headroom.
//
// This is mathematically the dual of a token bucket: same code path, same
// CAS loop, same bit layout, only the interpretation of the packed field
// differs. See the token bucket implementation for the dual framing.
//// TODO(study): address the following known issues when revisiting:
//   - TTL reset in getOrCreate races with consume (use CAS, not Store)
//   - refillFP multiplication can overflow on long idle + high leakRate
//   - capacity*1000 can overflow uint32 for very large capacity
//   - now-ts32 underflow on clock skew is guarded in consume but not getOrCreate
//   - maxBuckets is declared but not enforced (no eviction path)
package policy

import (
	"sync"
	"sync/atomic"
	"time"
)

// Bit-pack helpers

func pack_leaky(ts uint32, headroomFP uint32) uint64 {
	return (uint64(ts) << 32) | uint64(headroomFP)
}

func unpack_leaky(v uint64) (ts uint32, headroomFP uint32) {
	return uint32(v >> 32), uint32(v)
}

func nowSecs_leaky() uint32 {
	return uint32(time.Now().Unix())
}


// Shard (256 shards → O(1) contention under high concurrency)

const numShards = 256

type shard struct {
	mu      sync.RWMutex
	buckets map[string]*atomic.Uint64 // key: IP string or CIDR string
}

// LeakyBucketLimiter

// LeakyBucketLimiter is the per-IP rate limiter. It is safe for concurrent use.
//
// A request is admitted if the bucket has at least one drop of headroom.
// Otherwise it is rejected ("overflow"). This is the *policer* variant of a
// leaky bucket: excess is dropped, not queued. For a true shaper (queue +
// constant-rate drain), a different concurrency model is required.
type LeakyBucketLimiter struct {
	shards     [numShards]shard
	capacity   uint64 // max water level (burst size)
	leakRate   uint64 // drops leaked per second
	ttlSecs    uint32 // idle TTL after which a bucket entry is lazily reset
	maxBuckets uint64 // soft cap TODO: enforce via background eviction
}

// NewLeakyBucketLimiter creates a limiter with the given parameters.
//   - capacity:   maximum water level (burst size, in drops)
//   - leakRate:   drops leaked (drained) per second
//   - ttlSecs:    idle TTL after which a bucket entry is lazily reset
//   - maxBuckets: soft cap — oldest buckets are evicted when exceeded
func NewLeakyBucketLimiter(capacity, leakRate uint64, ttlSecs uint32, maxBuckets uint64) *LeakyBucketLimiter {
	l := &LeakyBucketLimiter{
		capacity:   capacity,
		leakRate:   leakRate,
		ttlSecs:    ttlSecs,
		maxBuckets: maxBuckets,
	}
	for i := range l.shards {
		l.shards[i].buckets = make(map[string]*atomic.Uint64)
	}
	return l
}
