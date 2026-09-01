package radixipecho

// from_yaml.go — hot-reload integration for Echo via fsnotify.
//
// Usage:
//
//	handler, stop, err := radixipecho.NewFromYAML("radixip.yaml", engineAdapter)
//	if err != nil { log.Fatal(err) }
//	defer stop()
//	e.Use(handler)

import (
	"log"
	"sync/atomic"

	"github.com/Mwangi-Derrick/radixip/lib/go/config"
	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
	"github.com/labstack/echo/v4"
)

type echoYAMLState struct {
	cfg     *config.RadixIpConfig
	limiter *policy.TokenBucketLimiter
}

func newEchoYAMLState(cfg *config.RadixIpConfig) *echoYAMLState {
	rl := cfg.RadixIP.RateLimit
	lim := policy.NewTokenBucketLimiter(
		rl.Capacity,
		rl.RefillRate,
		rl.TTLSeconds,
		rl.MaxBuckets,
	)
	log.Printf("radixipecho: (re)built limiter capacity=%d refill=%d/s", rl.Capacity, rl.RefillRate)
	return &echoYAMLState{cfg: cfg, limiter: lim}
}

type echoWatcher struct {
	watcher *config.Watcher
	state   atomic.Pointer[echoYAMLState]
	engine  Engine
}

func (g *echoWatcher) handle(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		latest := g.watcher.Current()
		s := g.state.Load()

		if s.cfg != latest {
			ns := newEchoYAMLState(latest)
			g.state.Store(ns)
			s = ns
		}

		mwCfg := latest.RadixIP.Middleware
		trusted := parseCIDRs(mwCfg.TrustedProxies)

		ip, err := policy.ExtractIP(c.Request(), trusted)
		if err != nil {
			return c.JSON(400, map[string]string{"error": "invalid ip"})
		}
		ipStr := ip.String()

		if latest.RadixIP.Blocklist.Enabled && g.engine != nil {
			if g.engine.Lookup(ipStr) {
				return c.JSON(mwCfg.Responses.Blocked, map[string]string{
					"error": "blocked",
					"ip":    ipStr,
				})
			}
		}

		if latest.RadixIP.RateLimit.Enabled && s.limiter != nil {
			key := bucketKey(ip, latest.RadixIP.RateLimit.BucketMode.Mode)
			if !s.limiter.Allow(key) {
				c.Response().Header().Set("Retry-After", "1")
				return c.JSON(mwCfg.Responses.RateLimited, map[string]string{
					"error": "rate limited",
					"ip":    ipStr,
				})
			}
		}

		return next(c)
	}
}

// NewFromYAML creates a hot-reloading Echo middleware from a YAML config file.
func NewFromYAML(path string, engine Engine) (echo.MiddlewareFunc, func(), error) {
	w, err := config.NewWatcher(path)
	if err != nil {
		return nil, nil, err
	}

	gw := &echoWatcher{watcher: w, engine: engine}
	gw.state.Store(newEchoYAMLState(w.Current()))

	return gw.handle, w.Stop, nil
}
