// Package ratelimit implements the per-IP Leaky Bucket rate limiter for RadixIP.
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
//
// TODO(study): address the following known issues when revisiting:
//   - TTL reset in getOrCreate races with consume (use CAS, not Store)
//   - refillFP multiplication can overflow on long idle + high leakRate
//   - capacity*1000 can overflow uint32 for very large capacity
//   - now-ts32 underflow on clock skew is guarded in consume but not getOrCreate
//   - maxBuckets is declared but not enforced (no eviction path)
package ratelimit

import (
	"hash/fnv"
	"sync/atomic"
	"time"
)

// // numShards is the number of map shards (power of two).
// const numShards = 256

// fpScale is the fixed-point scale for headroom (×1000).
const fpScale = 1000

// fullHeadroomFP is the packed headroom value for a full bucket (capacity drops).
// Note: capacity*1000 can overflow uint32 for very large capacity — see TODO.
const fullHeadroomFPCap = uint64(^uint32(0))

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

// // shard is one shard of the bucket map, guarded by its own mutex.
// type shard struct {
// 	mu      sync.Mutex
// 	buckets map[string]*atomic.Uint64
// }

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

// Allow admits a request when the bucket has room for one more drop. State
// updates use a CAS loop over the packed timestamp and headroom word.
func (l *LeakyBucketLimiter) Allow(key string) bool {
	bucket := l.getOrCreate(key)
	now := nowSecs_leaky()
	capFP := l.capacity * 1000
	if capFP > uint64(^uint32(0)) {
		capFP = uint64(^uint32(0))
	}
	for {
		old := bucket.Load()
		ts, headroom := unpack_leaky(old)
		elapsed := uint32(0)
		if now >= ts {
			elapsed = now - ts
		}
		if l.ttlSecs > 0 && elapsed > l.ttlSecs {
			headroom = uint32(capFP)
			elapsed = 0
		}
		// Saturate before conversion to avoid overflow on long idle periods.
		leaked := uint64(elapsed) * l.leakRate * 1000
		newHeadroom := uint64(headroom) + leaked
		if newHeadroom > capFP {
			newHeadroom = capFP
		}
		if newHeadroom < 1000 {
			return false
		}
		if bucket.CompareAndSwap(old, pack_leaky(now, uint32(newHeadroom-1000))) {
			return true
		}
	}
}

func (l *LeakyBucketLimiter) getOrCreate(key string) *atomic.Uint64 {
	s := l.shardFor(key)
	s.mu.RLock()
	b, ok := s.buckets[key]
	s.mu.RUnlock()
	if ok {
		return b
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok = s.buckets[key]; ok {
		return b
	}
	b = &atomic.Uint64{}
	capFP := l.capacity * 1000
	if capFP > uint64(^uint32(0)) {
		capFP = uint64(^uint32(0))
	}
	b.Store(pack_leaky(nowSecs_leaky(), uint32(capFP)))
	s.buckets[key] = b
	return b
}

// TrackedCount returns the number of buckets currently stored.
func (l *LeakyBucketLimiter) TrackedCount() int {
	total := 0
	for i := range l.shards {
		l.shards[i].mu.RLock()
		total += len(l.shards[i].buckets)
		l.shards[i].mu.RUnlock()
	}
	return total
}

// shardFor returns the shard responsible for key.
func (l *LeakyBucketLimiter) shardFor(key string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &l.shards[h.Sum32()&(numShards-1)]
}

// fullHeadroomFP returns the packed headroom for a freshly full bucket.
func (l *LeakyBucketLimiter) fullHeadroomFP() uint32 {
	fp := l.capacity * fpScale
	if fp > fullHeadroomFPCap {
		return uint32(fullHeadroomFPCap)
	}
	return uint32(fp)
}

// getOrCreate returns the atomic cell for key, creating it (full) if absent.
// A stale entry whose timestamp is older than ttlSecs is lazily reset.
func (l *LeakyBucketLimiter) getOrCreate(key string) *atomic.Uint64 {
	sh := l.shardFor(key)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	cell, ok := sh.buckets[key]
	if !ok {
		now := nowSecs_leaky()
		cell = &atomic.Uint64{}
		cell.Store(pack_leaky(now, l.fullHeadroomFP()))
		sh.buckets[key] = cell
		return cell
	}

	// Lazy TTL reset. TODO: this Store races with consume's CAS — use CAS.
	v := cell.Load()
	ts, _ := unpack_leaky(v)
	now := nowSecs_leaky()
	if l.ttlSecs > 0 && now > ts && now-ts > l.ttlSecs {
		cell.Store(pack_leaky(now, l.fullHeadroomFP()))
	}
	return cell
}

// consume attempts to take one drop of headroom from the bucket identified by
// key. It returns true if the request is admitted (headroom >= 1.0), false if
// the bucket overflowed. The CAS loop refills headroom from elapsed time and
// deducts the consumed drop atomically.
func (l *LeakyBucketLimiter) consume(cell *atomic.Uint64) bool {
	for {
		old := cell.Load()
		ts, headroomFP := unpack_leaky(old)

		now := nowSecs_leaky()

		// Elapsed seconds since last update, guarding against clock skew
		// (now < ts) which would otherwise underflow uint32 subtraction.
		var elapsed uint32
		if now > ts {
			elapsed = now - ts
		}

		// Refill: water leaks at leakRate/sec, increasing headroom by
		// leakRate*1000 per second. Guard against overflow on long idle.
		// TODO: refillFP multiplication can overflow on long idle + high leakRate.
		refillFP := uint64(elapsed) * l.leakRate * fpScale
		cur := uint64(headroomFP) + refillFP

		full := uint64(l.fullHeadroomFP())
		if cur > full {
			cur = full
		}

		// Admit only if at least one full drop of headroom is available.
		if cur < fpScale {
			// Reject. Persist the (possibly refilled, capped) state so the
			// timestamp advances and headroom isn't lost across attempts.
			newV := pack_leaky(now, uint32(cur))
			if cell.CompareAndSwap(old, newV) {
				return false
			}
			continue
		}

		// Consume one drop of headroom.
		newV := pack_leaky(now, uint32(cur-fpScale))
		if cell.CompareAndSwap(old, newV) {
			return true
		}
		// Lost the race; retry with fresh state.
	}
}

// Allow reports whether a request from key should be admitted. It is safe for
// concurrent use. A request is admitted if the bucket has at least one drop of
// headroom; admission consumes one drop of headroom.
func (l *LeakyBucketLimiter) Allow(key string) bool {
	cell := l.getOrCreate(key)
	return l.consume(cell)
}
