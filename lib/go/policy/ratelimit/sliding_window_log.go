package ratelimit

import (
	"sync"
	"time"
)

type slidingLogState struct {
	mu      sync.Mutex
	entries []time.Time
}

// SlidingWindowLog preserves exact timestamps. Each key has its own mutex so
// unrelated clients no longer serialize on one limiter-wide lock.
type SlidingWindowLog struct {
	shards [numShards]slidingLogShard
	limit  int
	window time.Duration
}

type slidingLogShard struct {
	mu     sync.RWMutex
	states map[string]*slidingLogState
}

func NewSlidingWindowLog(limit int, window time.Duration) *SlidingWindowLog {
	s := &SlidingWindowLog{limit: limit, window: window}
	for i := range s.shards {
		s.shards[i].states = make(map[string]*slidingLogState)
	}
	return s
}

func (s *SlidingWindowLog) Allow(key string, now time.Time) bool {
	if s.limit <= 0 || s.window <= 0 {
		return false
	}
	h := fnv32(key)
	sh := &s.shards[h%numShards]
	sh.mu.RLock()
	state := sh.states[key]
	sh.mu.RUnlock()
	if state == nil {
		sh.mu.Lock()
		state = sh.states[key]
		if state == nil {
			state = &slidingLogState{}
			sh.states[key] = state
		}
		sh.mu.Unlock()
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	cutoff := now.Add(-s.window)
	entries := state.entries
	first := 0
	for first < len(entries) && !entries[first].After(cutoff) {
		first++
	}
	if first != 0 {
		entries = append(entries[:0], entries[first:]...)
	}
	if len(entries) >= s.limit {
		state.entries = entries
		return false
	}
	state.entries = append(entries, now)
	return true
}
