// Package ratelimit implements the per-key leaky-bucket policer.
package ratelimit

import (
	"sync/atomic"
	"time"
)

const leakyScale uint64 = 1000

func saturatingMul(a, b uint64) uint64 {
	if a != 0 && b > ^uint64(0)/a {
		return ^uint64(0)
	}
	return a * b
}

func saturatingAdd(a, b uint64) uint64 {
	if b > ^uint64(0)-a {
		return ^uint64(0)
	}
	return a + b
}

func packLeaky(ts, headroom uint32) uint64 {
	return uint64(ts)<<32 | uint64(headroom)
}

func unpackLeaky(v uint64) (ts, headroom uint32) {
	return uint32(v >> 32), uint32(v)
}

func leakyNowSecs() uint32 { return uint32(time.Now().Unix()) }

// LeakyBucketLimiter stores one atomic packed timestamp/headroom value for
// each key. Requests that exceed capacity are rejected rather than queued.
type LeakyBucketLimiter struct {
	shards     [numShards]shard
	capacity   uint64
	leakRate   uint64
	ttlSecs    uint32
	maxBuckets uint64
}

// NewLeakyBucketLimiter creates a limiter with a maximum burst capacity,
// leak rate in requests per second, idle reset TTL, and soft bucket limit.
func NewLeakyBucketLimiter(capacity, leakRate uint64, ttlSecs uint32, maxBuckets uint64) *LeakyBucketLimiter {
	l := &LeakyBucketLimiter{
		capacity: capacity, leakRate: leakRate, ttlSecs: ttlSecs, maxBuckets: maxBuckets,
	}
	for i := range l.shards {
		l.shards[i].buckets = make(map[string]*atomic.Uint64)
	}
	return l
}

// Allow admits a request if at least one request of headroom is available.
// Refill, TTL reset, and consumption commit together with a CAS operation.
func (l *LeakyBucketLimiter) Allow(key string) bool {
	cell := l.getOrCreate(key)
	full := l.capacityFixedPoint()
	for {
		old := cell.Load()
		ts, rawHeadroom := unpackLeaky(old)
		now := leakyNowSecs()
		effectiveNow := now
		if effectiveNow < ts { // avoid moving state backward after clock skew
			effectiveNow = ts
		}

		headroom := uint64(rawHeadroom)
		elapsed := uint64(0)
		if now >= ts {
			elapsed = uint64(now - ts)
		}
		if l.ttlSecs > 0 && elapsed > uint64(l.ttlSecs) {
			headroom = full
			elapsed = 0
		}

		// Saturating arithmetic prevents overflow during long idle periods or
		// with unusually high configured leak rates.
		leaked := saturatingMul(elapsed, l.leakRate)
		leaked = saturatingMul(leaked, leakyScale)
		headroom = min(full, saturatingAdd(headroom, leaked))
		updatedHeadroom := headroom
		allowed := headroom >= leakyScale
		if allowed {
			updatedHeadroom -= leakyScale
		}
		if cell.CompareAndSwap(old, packLeaky(effectiveNow, uint32(updatedHeadroom))) {
			return allowed
		}
	}
}

func (l *LeakyBucketLimiter) capacityFixedPoint() uint64 {
	if l.capacity > uint64(^uint32(0))/leakyScale {
		return uint64(^uint32(0))
	}
	return l.capacity * leakyScale
}

func (l *LeakyBucketLimiter) getOrCreate(key string) *atomic.Uint64 {
	h := fnv32(key)
	s := &l.shards[h%numShards]
	s.mu.RLock()
	cell := s.buckets[key]
	s.mu.RUnlock()
	if cell != nil {
		return cell
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cell = s.buckets[key]; cell == nil {
		cell = &atomic.Uint64{}
		cell.Store(packLeaky(leakyNowSecs(), uint32(l.capacityFixedPoint())))
		s.buckets[key] = cell
	}
	return cell
}

// TrackedCount reports the number of initialized per-key buckets.
func (l *LeakyBucketLimiter) TrackedCount() int {
	total := 0
	for i := range l.shards {
		l.shards[i].mu.RLock()
		total += len(l.shards[i].buckets)
		l.shards[i].mu.RUnlock()
	}
	return total
}
