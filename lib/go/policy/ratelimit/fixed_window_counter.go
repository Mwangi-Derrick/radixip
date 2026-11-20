package ratelimit

import (
	"sync/atomic"
	"time"
)

// FixedWindowCounter keeps one packed (window ID, count) word per key. Map
// creation is protected by the existing shard lock; steady-state updates use
// CAS and do not take a mutex.
type FixedWindowCounter struct {
	shards [numShards]shard
	limit  uint32
	window time.Duration
}

func NewFixedWindowCounter(limit int, window time.Duration) *FixedWindowCounter {
	if limit < 0 {
		limit = 0
	}
	if uint64(limit) > uint64(^uint32(0)) {
		limit = int(^uint32(0))
	}
	f := &FixedWindowCounter{limit: uint32(limit), window: window}
	for i := range f.shards {
		f.shards[i].buckets = make(map[string]*atomic.Uint64)
	}
	return f
}

func (f *FixedWindowCounter) Allow(key string, now time.Time) bool {
	if f.limit == 0 || f.window <= 0 {
		return false
	}
	windowID := uint32(now.UnixNano() / f.window.Nanoseconds())
	cell := getOrCreateCounterCell(&f.shards, key)
	for {
		old := cell.Load()
		oldWindow, count := uint32(old>>32), uint32(old)
		if oldWindow != windowID {
			count = 0
		}
		if count >= f.limit {
			return false
		}
		updated := uint64(windowID)<<32 | uint64(count+1)
		if cell.CompareAndSwap(old, updated) {
			return true
		}
	}
}

func getOrCreateCounterCell(shards *[numShards]shard, key string) *atomic.Uint64 {
	h := fnv32(key)
	s := &shards[h%numShards]
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
		s.buckets[key] = cell
	}
	return cell
}
