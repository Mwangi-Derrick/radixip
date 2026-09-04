package radixipgin

// from_yaml.go — hot-reload integration for Gin via fsnotify.
//
// Usage:
//
//	handler, stop, err := radixipgin.NewFromYAML("radixip.yaml", engineAdapter)
//	if err != nil { log.Fatal(err) }
//	defer stop()
//	r.Use(handler)

import (
	"log"
	"sync/atomic"

	"github.com/Mwangi-Derrick/radixip/lib/go/config"
	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
	"github.com/gin-gonic/gin"
)

// yamlState bundles a config snapshot with the limiter derived from it.
type yamlState struct {
	cfg       *config.RadixIpConfig
	limiter   *policy.TokenBucketLimiter
	routeTrie *policy.RouteTrieNode
	autoBan   *policy.AutoBanTracker
}

func newYAMLState(cfg *config.RadixIpConfig, eng Engine) *yamlState {
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
	log.Printf("radixipgin: (re)built limiter capacity=%d refill=%d/s auto_ban=%v route_trie=%v",
		rl.Capacity, rl.RefillRate, cfg.RadixIP.AutoBan.Enabled, rt != nil)
	return &yamlState{cfg: cfg, limiter: lim, routeTrie: rt, autoBan: ban}
}

// ginWatcher holds the atomic hot-swap state.
type ginWatcher struct {
	watcher *config.Watcher
	state   atomic.Pointer[yamlState]
	engine  Engine
}

func (g *ginWatcher) ServeHTTP(c *gin.Context) {
	latest := g.watcher.Current()
	s := g.state.Load()

	// Config pointer changed → rebuild limiter.
	if s.cfg != latest {
		next := newYAMLState(latest, g.engine)
		g.state.Store(next)
		s = next
	}

	mwCfg := latest.RadixIP.Middleware
	trusted := parseCIDRs(mwCfg.TrustedProxies)

	ip, err := policy.ExtractIP(c.Request, trusted)
	if err != nil {
		c.AbortWithStatusJSON(400, gin.H{"error": err.Error()})
		return
	}
	ipStr := ip.String()

	// Blocklist check.
	if latest.RadixIP.Blocklist.Enabled && g.engine != nil {
		if g.engine.Lookup(ipStr) {
			c.AbortWithStatusJSON(mwCfg.Responses.Blocked, gin.H{
				"error": "blocked",
				"ip":    ipStr,
			})
			return
		}
	}

	// Rate limit check.
	if latest.RadixIP.RateLimit.Enabled && s.limiter != nil {
		key := bucketKey(ip, latest.RadixIP.RateLimit.BucketMode.Mode)
		
		var allowed bool
		if s.routeTrie != nil {
			if routeLimiter := s.routeTrie.Match(c.Request.URL.Path, c.Request.Method); routeLimiter != nil {
				allowed = routeLimiter.Allow(key)
			} else {
				allowed = s.limiter.Allow(key)
			}
		} else {
			allowed = s.limiter.Allow(key)
		}

		if !allowed {
			// Auto-ban: record violation; if threshold hit, engine already has the ban.
			if s.autoBan != nil && s.autoBan.RecordViolation(ipStr) {
				c.AbortWithStatusJSON(mwCfg.Responses.Blocked, gin.H{
					"error": "auto-banned",
					"ip":    ipStr,
				})
				return
			}
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(mwCfg.Responses.RateLimited, gin.H{
				"error": "rate limited",
				"ip":    ipStr,
			})
			return
		}
	}

	c.Next()
}

// NewFromYAML creates a hot-reloading Gin middleware from a YAML config file.
//
// The engine adapter is passed separately because the ART tree state (blocklist
// data) is managed outside the config lifecycle. Only rate-limit parameters and
// middleware options are hot-swapped when the file changes.
//
// stop() must be called on server shutdown to release fsnotify resources.
// NewFromYAML creates a hot-reloading Gin middleware from a YAML config file.
func NewFromYAML(path string, engine Engine) (gin.HandlerFunc, func(), error) {
	w, err := config.NewWatcher(path)
	if err != nil {
		return nil, nil, err
	}

	gw := &ginWatcher{watcher: w, engine: engine}
	gw.state.Store(newYAMLState(w.Current(), engine))

	return gw.ServeHTTP, w.Stop, nil
}
