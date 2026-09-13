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

