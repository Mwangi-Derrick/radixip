// Package radixipnethttp provides RadixIP middleware for net/http-compatible routers.
package radixipnethttp

// from_yaml.go — hot-reload integration for net/http via fsnotify.
//
// Usage:
//
//	handler, stop, err := radixipnethttp.NewFromYAML("radixip.yaml", engineAdapter)
//	if err != nil { log.Fatal(err) }
//	defer stop()
//	mux.Handle("/", handler)
//
// The returned handler is a plain http.Handler, so it composes with any
// router built on net/http (chi, gorilla/mux, httprouter, stdlib ServeMux).

import (
	"log"
	"net/http"
	"sync/atomic"

	"github.com/Mwangi-Derrick/radixip/lib/go/config"
	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
)

// yamlState bundles a config snapshot with the limiter derived from it.
type yamlState struct {
	cfg       *config.RadixIpConfig
	limiter   *policy.TokenBucketLimiter
	routeTrie *policy.RouteTrieNode
	autoBan   *policy.AutoBanTracker
}

// newYAMLState builds a limiter, route trie, and auto-ban tracker from a
// config snapshot. It is called once at construction and again whenever the
// watcher observes a file change.
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
	log.Printf("radixipnethttp: (re)built limiter capacity=%d refill=%d/s auto_ban=%v route_trie=%v",
		rl.Capacity, rl.RefillRate, cfg.RadixIP.AutoBan.Enabled, rt != nil)
	return &yamlState{cfg: cfg, limiter: lim, routeTrie: rt, autoBan: ban}
}

// watcherHandler holds the atomic hot-swap state and implements http.Handler.
type watcherHandler struct {
	watcher *config.Watcher
	state   atomic.Pointer[yamlState]
	engine  Engine
}

// ServeHTTP implements http.Handler. On each request it checks whether the
// config pointer has changed and, if so, rebuilds the limiter before
// applying the blocklist / rate-limit checks.
func (h *watcherHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	latest := h.watcher.Current()
	s := h.state.Load()

	// Config pointer changed → rebuild limiter.
	if s.cfg != latest {
		next := newYAMLState(latest, h.engine)
		h.state.Store(next)
		s = next
	}

	mwCfg := latest.RadixIP.Middleware
	trusted := parseCIDRs(mwCfg.TrustedProxies)

	ip, err := policy.ExtractIP(r, trusted)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ipStr := ip.String()

	// Blocklist check.
	if latest.RadixIP.Blocklist.Enabled && h.engine != nil {
		if h.engine.Lookup(ipStr) {
			writeJSON(w, mwCfg.Responses.Blocked, map[string]string{
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
			if routeLimiter := s.routeTrie.Match(r.URL.Path, r.Method); routeLimiter != nil {
				allowed = routeLimiter.Allow(key)
			} else {
				allowed = s.limiter.Allow(key)
			}
		} else {
			allowed = s.limiter.Allow(key)
		}

		if !allowed {
			// Auto-ban: record violation; if the threshold is hit, the
			// engine already holds the ban.
			if s.autoBan != nil && s.autoBan.RecordViolation(ipStr) {
				writeJSON(w, mwCfg.Responses.Blocked, map[string]string{
					"error": "auto-banned",
					"ip":    ipStr,
				})
				return
			}
			w.Header().Set("Retry-After", "1")
			writeJSON(w, mwCfg.Responses.RateLimited, map[string]string{
				"error": "rate limited",
				"ip":    ipStr,
			})
			return
		}
	}

	// All checks passed. In the net/http world there's no "c.Next()";
	// the handler simply returns and the router continues.
}

// NewFromYAML creates a hot-reloading net/http handler from a YAML config file.
//
// The engine adapter is passed separately because the ART tree state (blocklist
// data) is managed outside the config lifecycle. Only rate-limit parameters and
// middleware options are hot-swapped when the file changes.
//
// stop() must be called on server shutdown to release fsnotify resources.
//
// The returned http.Handler is a complete handler: mount it directly or wrap
// it with a router. To use it as chi middleware (i.e. one layer in a stack),
// use adapters/chi instead.
func NewFromYAML(path string, engine Engine) (http.Handler, func(), error) {
	w, err := config.NewWatcher(path)
	if err != nil {
		return nil, nil, err
	}

	h := &watcherHandler{watcher: w, engine: engine}
	h.state.Store(newYAMLState(w.Current(), engine))

	return h, w.Stop, nil
}

// MiddlewareFromYAML creates a hot-reloading net/http middleware from a YAML
// config file. It wraps the request pipeline and invokes the downstream handler
// only after the configured blocklist/rate-limit checks pass.
func MiddlewareFromYAML(path string, engine Engine) (func(http.Handler) http.Handler, func(), error) {
	w, err := config.NewWatcher(path)
	if err != nil {
		return nil, nil, err
	}

	h := &watcherHandler{watcher: w, engine: engine}
	h.state.Store(newYAMLState(w.Current(), engine))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			latest := h.watcher.Current()
			s := h.state.Load()
			if s.cfg != latest {
				nextState := newYAMLState(latest, h.engine)
				h.state.Store(nextState)
				s = nextState
			}

			mwCfg := latest.RadixIP.Middleware
			trusted := parseCIDRs(mwCfg.TrustedProxies)

			ip, err := policy.ExtractIP(r, trusted)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			ipStr := ip.String()

			if latest.RadixIP.Blocklist.Enabled && h.engine != nil {
				if h.engine.Lookup(ipStr) {
					writeJSON(w, mwCfg.Responses.Blocked, map[string]string{
						"error": "blocked",
						"ip":    ipStr,
					})
					return
				}
			}

			if latest.RadixIP.RateLimit.Enabled && s.limiter != nil {
				key := bucketKey(ip, latest.RadixIP.RateLimit.BucketMode.Mode)

				var allowed bool
				if s.routeTrie != nil {
					if routeLimiter := s.routeTrie.Match(r.URL.Path, r.Method); routeLimiter != nil {
						allowed = routeLimiter.Allow(key)
					} else {
						allowed = s.limiter.Allow(key)
					}
				} else {
					allowed = s.limiter.Allow(key)
				}

				if !allowed {
					if s.autoBan != nil && s.autoBan.RecordViolation(ipStr) {
						writeJSON(w, mwCfg.Responses.Blocked, map[string]string{
							"error": "auto-banned",
							"ip":    ipStr,
						})
						return
					}
					w.Header().Set("Retry-After", "1")
					writeJSON(w, mwCfg.Responses.RateLimited, map[string]string{
						"error": "rate limited",
						"ip":    ipStr,
					})
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}, w.Stop, nil
}
