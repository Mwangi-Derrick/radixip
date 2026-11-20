package ratelimit

import (
	"sync"
	"time"
)

type slidingCounterState struct {
	currWindowStart time.Time
	currCount       int
	prevCount       int
}

type SlidingWindowCounter struct {
	mu     sync.Mutex
	limit  float64
	window time.Duration
	states map[string]*slidingCounterState
}

func NewSlidingWindowCounter(limit int, window time.Duration) *SlidingWindowCounter {
	return &SlidingWindowCounter{
		limit:  float64(limit),
		window: window,
		states: make(map[string]*slidingCounterState),
	}
}

func (s *SlidingWindowCounter) Allow(key string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	winStart := now.Truncate(s.window)
	st, ok := s.states[key]
	if !ok {
		st = &slidingCounterState{
			currWindowStart: winStart,
			currCount:       1,
			prevCount:       0,
		}
		s.states[key] = st
		return float64(st.currCount) <= s.limit
	}

	if winStart.After(st.currWindowStart) {
		// Advance windows; if one or more full windows were skipped, previous count is 0.
		if winStart.Sub(st.currWindowStart) == s.window {
			st.prevCount = st.currCount
		} else {
			st.prevCount = 0
		}
		st.currWindowStart = winStart
		st.currCount = 0
	}

	elapsed := now.Sub(st.currWindowStart).Seconds()
	weight := 1.0 - (elapsed / s.window.Seconds())
	if weight < 0 {
		weight = 0
	}

	estimated := float64(st.prevCount)*weight + float64(st.currCount)
	if estimated+1.0 > s.limit {
		return false
	}

	st.currCount++
	return true
}