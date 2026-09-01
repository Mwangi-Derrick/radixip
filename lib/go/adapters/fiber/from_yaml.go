package radixipfiber

// from_yaml.go — hot-reload integration for Fiber via fsnotify.
//
// Usage:
//
//	handler, stop, err := radixipfiber.NewFromYAML("radixip.yaml", engineAdapter)
//	if err != nil { log.Fatal(err) }
//	defer stop()
//	app.Use(handler)

import (
	"log"
	"sync/atomic"

	"github.com/Mwangi-Derrick/radixip/lib/go/config"
	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
	"github.com/gofiber/fiber/v2"
)

type fiberYAMLState struct {
	cfg     *config.RadixIpConfig
	limiter *policy.TokenBucketLimiter
}

func newFiberYAMLState(cfg *config.RadixIpConfig) *fiberYAMLState {
	rl := cfg.RadixIP.RateLimit
	lim := policy.NewTokenBucketLimiter(
		rl.Capacity,
		rl.RefillRate,
		rl.TTLSeconds,
		rl.MaxBuckets,
	)
	log.Printf("radixipfiber: (re)built limiter capacity=%d refill=%d/s", rl.Capacity, rl.RefillRate)
	return &fiberYAMLState{cfg: cfg, limiter: lim}
}

type fiberWatcher struct {
	watcher *config.Watcher
	state   atomic.Pointer[fiberYAMLState]
	engine  Engine
}

func (fw *fiberWatcher) handle(c *fiber.Ctx) error {
	latest := fw.watcher.Current()
	s := fw.state.Load()

	if s.cfg != latest {
		ns := newFiberYAMLState(latest)
		fw.state.Store(ns)
		s = ns
	}

	mwCfg := latest.RadixIP.Middleware
	trusted := parseCIDRs(mwCfg.TrustedProxies)

	ip := extractIPFiber(c, trusted)
	if ip == nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid ip"})
	}
	ipStr := ip.String()

	if latest.RadixIP.Blocklist.Enabled && fw.engine != nil {
		if fw.engine.Lookup(ipStr) {
			return c.Status(mwCfg.Responses.Blocked).JSON(fiber.Map{
				"error": "blocked",
				"ip":    ipStr,
			})
		}
	}

	if latest.RadixIP.RateLimit.Enabled && s.limiter != nil {
		key := bucketKey(ip, latest.RadixIP.RateLimit.BucketMode.Mode)
		if !s.limiter.Allow(key) {
			c.Set("Retry-After", "1")
			return c.Status(mwCfg.Responses.RateLimited).JSON(fiber.Map{
				"error": "rate limited",
				"ip":    ipStr,
			})
		}
	}

	return c.Next()
}

// NewFromYAML creates a hot-reloading Fiber middleware from a YAML config file.
func NewFromYAML(path string, engine Engine) (fiber.Handler, func(), error) {
	w, err := config.NewWatcher(path)
	if err != nil {
		return nil, nil, err
	}

	fw := &fiberWatcher{watcher: w, engine: engine}
	fw.state.Store(newFiberYAMLState(w.Current()))

	return fw.handle, w.Stop, nil
}
