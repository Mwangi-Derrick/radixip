package policy

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	config "github.com/Mwangi-Derrick/radixip/lib/go/config"
	engine "github.com/Mwangi-Derrick/radixip/lib/go/engine"
)

type AutoBanTracker struct {
	mu          sync.RWMutex
	violations  map[string][]time.Time
	threshold   uint64
	window      time.Duration
	banDuration time.Duration
	engine      *engine.EngineWrapper
}

func NewAutoBanTracker(cfg config.AutoBanConfig, engine *engine.EngineWrapper) *AutoBanTracker {
	return &AutoBanTracker{
		violations:  make(map[string][]time.Time),
		threshold:   cfg.ThresholdViolations,
		window:      time.Duration(cfg.WindowSeconds),
		banDuration: time.Duration(cfg.BanDurationSeconds),
		engine:      engine,
	}
}

func (a *AutoBanTracker) RecordViolation(ipStr string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Add current violation
	now := time.Now()
	a.violations[ipStr] = append(a.violations[ipStr], now)

	// Clean up old violations
	windowStart := now.Add(-a.window)
	filtered := []time.Time{}
	for _, t := range a.violations[ipStr] {
		if t.After(windowStart) {
			filtered = append(filtered, t)
		}
	}
	a.violations[ipStr] = filtered

	// Check if violation count exceeds threshold
	if uint64(len(filtered)) >= a.threshold {
		// Auto-ban the IP
		// ParseCIDR dynamically reads the IP and the mask from the string
		_, ipNet, err := net.ParseCIDR(ipStr)
		if err != nil {
			fmt.Println("Error parsing CIDR:", err)
			return false
		}

		a.engine.Insert(ipNet, engine.Metadata{
			Value: "auto-banned",
			Attributes: map[string]string{
				"reason": "exceeded_threshold",
				"count":  strconv.FormatUint(a.threshold, 10),
				"window": a.window.String(),
			},
		})
		return true
	}

	return false
}
