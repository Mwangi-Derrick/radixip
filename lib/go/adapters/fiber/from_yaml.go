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
	cfg       *config.RadixIpConfig
	limiter   *policy.TokenBucketLimiter
	routeTrie *policy.RouteTrieNode
	autoBan   *policy.AutoBanTracker
}

func newFiberYAMLState(cfg *config.RadixIpConfig, eng Engine) *fiberYAMLState {
	rl := cfg.RadixIP.RateLimit
	lim := policy.NewTokenBucketLimiter(
		rl.Capacity,
		rl.RefillRate,
		rl.TTLSeconds,
		rl.MaxBuckets,
	)
	var rt *policy.RouteTrieNode
	if cfg.RadixIP.RateLimitRoutes.Enabled {
		rt = policy.NewRouteTrie()
		for _, route := range cfg.RadixIP.RateLimitRoutes.Routes {
			for _, method := range route.Methods {
				rt.AddRoute(route.Path, method, route.RateLimit)
			}
		}
	}

	var ban *policy.AutoBanTracker
	if cfg.RadixIP.AutoBan.Enabled {
		if be, ok := eng.(policy.BanEngine); ok {
			ban = policy.NewAutoBanTracker(cfg.RadixIP.AutoBan, be)
		}
	}
	log.Printf("radixipfiber: (re)built limiter capacity=%d refill=%d/s route_trie=%v", rl.Capacity, rl.RefillRate, rt != nil)
	return &fiberYAMLState{cfg: cfg, limiter: lim, routeTrie: rt, autoBan: ban}
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
		ns := newFiberYAMLState(latest, fw.engine)
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
		
		var allowed bool
		if s.routeTrie != nil {
			if routeLimiter := s.routeTrie.Match(c.Path(), c.Method()); routeLimiter != nil {
				allowed = routeLimiter.Allow(key)
			} else {
				allowed = s.limiter.Allow(key)
			}
		} else {
			allowed = s.limiter.Allow(key)
		}

		if !allowed {
			if s.autoBan != nil && s.autoBan.RecordViolation(ipStr) {
				return c.Status(mwCfg.Responses.Blocked).JSON(fiber.Map{
					"error": "auto-banned",
					"ip":    ipStr,
				})
			}
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
	fw.state.Store(newFiberYAMLState(w.Current(), engine))

	return fw.handle, w.Stop, nil
}
