package ratelimit

import (
	"sync/atomic"
	"time"
)

// SlidingWindowCounter stores the current window ID and the previous/current
// counts in one word. Each count is 16 bits, so configured limits above 65535
// are capped to the representable range.
type SlidingWindowCounter struct {
	shards [numShards]shard
	limit  uint16
	window time.Duration
}

func NewSlidingWindowCounter(limit int, window time.Duration) *SlidingWindowCounter {
	if limit < 0 {
		limit = 0
	}
	if limit > int(^uint16(0)) {
		limit = int(^uint16(0))
	}
	s := &SlidingWindowCounter{limit: uint16(limit), window: window}
	for i := range s.shards {
		s.shards[i].buckets = make(map[string]*atomic.Uint64)
	}
	return s
}

func (s *SlidingWindowCounter) Allow(key string, now time.Time) bool {
	if s.limit == 0 || s.window <= 0 {
		return false
	}
	windowNanos := s.window.Nanoseconds()
	windowID64 := now.UnixNano() / windowNanos
	windowID := uint32(windowID64)
	elapsed := now.UnixNano() - windowID64*windowNanos
	cell := getOrCreateCounterCell(&s.shards, key)
	for {
		old := cell.Load()
		oldID := uint32(old >> 32)
		prev, curr := uint16(old>>16), uint16(old)
		if oldID != windowID {
			delta := uint32(windowID - oldID)
			switch delta {
			case 1:
				prev, curr = curr, 0
			default:
				prev, curr = 0, 0
			}
		}
		weight := float64(windowNanos-elapsed) / float64(windowNanos)
		if weight < 0 {
			weight = 0
		} else if weight > 1 {
			weight = 1
		}
		estimated := float64(prev)*weight + float64(curr)
		if estimated+1 > float64(s.limit) {
			return false
		}
		if curr < ^uint16(0) {
			curr++
		}
		updated := uint64(windowID)<<32 | uint64(prev)<<16 | uint64(curr)
		if cell.CompareAndSwap(old, updated) {
			return true
		}
	}
}
