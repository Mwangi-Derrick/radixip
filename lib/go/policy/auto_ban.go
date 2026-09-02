package policy

import (
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

func NewAutoBanTracker(cfg config.AutoBanConfig, engine *engine.EngineWrapper) *AutoBanTracker
func (a *AutoBanTracker) RecordViolation(ipStr string) bool // returns true if IP was auto-banned
