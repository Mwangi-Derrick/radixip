package policy

// auto_ban.go
// Per-IP violation tracking and automatic temporary ban injection.
//
// When an IP accumulates `threshold` violations within a sliding `window`, it is
// dynamically inserted into the RadixIP Engine blocklist with an "auto-banned"
// metadata value. The ban entry is expired by a background sweeper goroutine
// started on NewAutoBanTracker.

import (
	"net"
	"strconv"
	"sync"
	"time"

	config "github.com/Mwangi-Derrick/radixip/lib/go/config"
	engine "github.com/Mwangi-Derrick/radixip/lib/go/engine"
)

// BanEngine is the subset of the RadixIP Engine required by the AutoBanTracker.
type BanEngine interface {
	Insert(prefix *net.IPNet, meta engine.Metadata) error
	Remove(prefix *net.IPNet) *engine.Metadata
}

// AutoBanTracker records per-IP rate-limit violations and auto-bans repeat
// offenders by injecting them into the RadixIP blocklist tree.
type AutoBanTracker struct {
	mu          sync.Mutex
	violations  map[string][]time.Time // ipStr → sorted violation timestamps
	banned      map[string]time.Time   // ipStr → ban expiry time
	threshold   uint64
	window      time.Duration
	banDuration time.Duration
	engine      BanEngine
}

// NewAutoBanTracker creates and starts an AutoBanTracker backed by the given
// engine and config. The background sweeper goroutine is started automatically;
// call Stop() on shutdown.
func NewAutoBanTracker(cfg config.AutoBanConfig, eng BanEngine) *AutoBanTracker {
	t := &AutoBanTracker{
		violations:  make(map[string][]time.Time),
		banned:      make(map[string]time.Time),
		threshold:   cfg.ThresholdViolations,
		window:      time.Duration(cfg.WindowSeconds) * time.Second,
		banDuration: time.Duration(cfg.BanDurationSeconds) * time.Second,
		engine:      eng,
	}
	go t.sweeper()
	return t
}

// RecordViolation records a rate-limit violation for ipStr. If violations exceed
// the threshold within the sliding window, the IP is auto-banned and true is
// returned. Returns false otherwise.
func (a *AutoBanTracker) RecordViolation(ipStr string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()

	// Prune violations outside the window.
	windowStart := now.Add(-a.window)
	prev := a.violations[ipStr]
	filtered := prev[:0] // reuse slice memory
	for _, t := range prev {
		if t.After(windowStart) {
			filtered = append(filtered, t)
		}
	}
	filtered = append(filtered, now)
	a.violations[ipStr] = filtered

	if uint64(len(filtered)) >= a.threshold {
		a.ban(ipStr, now)
		return true
	}
	return false
}

// IsBanned reports whether ipStr is currently in a ban period managed by this
// tracker. Note: banned IPs are also in the engine blocklist, so the middleware
// blocklist check will catch them independently. This is a cheap local check.
func (a *AutoBanTracker) IsBanned(ipStr string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	expiry, ok := a.banned[ipStr]
	return ok && time.Now().Before(expiry)
}

// ban inserts ipStr into the engine blocklist and records the expiry.
// Must be called with a.mu held.
func (a *AutoBanTracker) ban(ipStr string, now time.Time) {
	expiry := now.Add(a.banDuration)
	a.banned[ipStr] = expiry
	// Reset violation counter so repeated floods don't keep re-banning.
	delete(a.violations, ipStr)

	// Build the /32 (IPv4) or /128 (IPv6) host prefix for the engine.
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return
	}
	var prefix *net.IPNet
	if ip.To4() != nil {
		_, prefix, _ = net.ParseCIDR(ipStr + "/32")
	} else {
		_, prefix, _ = net.ParseCIDR(ipStr + "/128")
	}
	if prefix == nil {
		return
	}
	_ = a.engine.Insert(prefix, engine.Metadata{
		Value: "auto-banned",
		Attributes: map[string]string{
			"reason":     "exceeded_threshold",
			"count":      strconv.FormatUint(a.threshold, 10),
			"window":     a.window.String(),
			"expires_at": expiry.UTC().Format(time.RFC3339),
		},
	})
}

// sweeper is a background goroutine that removes expired bans from the engine.
func (a *AutoBanTracker) sweeper() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for range tick.C {
		a.sweep()
	}
}

func (a *AutoBanTracker) sweep() {
	now := time.Now()
	a.mu.Lock()
	expired := make([]string, 0)
	for ipStr, expiry := range a.banned {
		// Treat bans whose expiry time is equal to or before now as expired.
		// Use !now.Before(expiry) instead of now.After(expiry) to include equality
		if !now.Before(expiry) {
			expired = append(expired, ipStr)
		}
	}
	for _, ipStr := range expired {
		delete(a.banned, ipStr)
	}
	a.mu.Unlock()

	// Remove from engine outside the lock to avoid deadlocking on engine ops.
	for _, ipStr := range expired {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		var prefix *net.IPNet
		if ip.To4() != nil {
			_, prefix, _ = net.ParseCIDR(ipStr + "/32")
		} else {
			_, prefix, _ = net.ParseCIDR(ipStr + "/128")
		}
		if prefix != nil {
			a.engine.Remove(prefix)
		}
	}
}
