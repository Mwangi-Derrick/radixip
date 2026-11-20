package ratelimit

import (
	"sync"
	"time"
)

type fixedWindowState struct {
	windowStart time.Time
	count       int
}

type FixedWindowCounter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	states map[string]*fixedWindowState
}

func NewFixedWindowCounter(limit int, window time.Duration) *FixedWindowCounter {
	return &FixedWindowCounter{
		limit:  limit,
		window: window,
		states: make(map[string]*fixedWindowState),
	}
}

func (f *FixedWindowCounter) Allow(key string, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	winStart := now.Truncate(f.window)
	st, ok := f.states[key]
	if !ok || !st.windowStart.Equal(winStart) {
		f.states[key] = &fixedWindowState{
			windowStart: winStart,
			count:       1,
		}
		return 1 <= f.limit
	}

	if st.count >= f.limit {
		return false
	}

	st.count++
	return true
}