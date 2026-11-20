package ratelimit

import (
	"sync"
	"time"
)

type SlidingWindowLog struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	logs   map[string][]time.Time
}

func NewSlidingWindowLog(limit int, window time.Duration) *SlidingWindowLog {
	return &SlidingWindowLog{
		limit:  limit,
		window: window,
		logs:   make(map[string][]time.Time),
	}
}

func (s *SlidingWindowLog) Allow(key string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := now.Add(-s.window)
	entries := s.logs[key]

	// Drop timestamps older than the rolling window.
	idx := 0
	for idx < len(entries) && !entries[idx].After(cutoff) {
		idx++
	}
	entries = entries[idx:]

	if len(entries) >= s.limit {
		s.logs[key] = entries
		return false
	}

	entries = append(entries, now)
	s.logs[key] = entries
	return true
}