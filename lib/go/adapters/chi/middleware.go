// Package chi provides RadixIP middleware for go-chi/chi.
//
// chi is built directly on net/http, so this package is a thin re-export
// of the net/http adapter. The middleware signature is identical and
// composes with chi's own middleware stack
// (github.com/go-chi/chi/v5/middleware).
//
// Recommended wiring:
//
//	r := chi.NewRouter()
//	r.Use(middleware.RealIP)               // resolve client IP into r.RemoteAddr
//	r.Use(chimw.Logger)
//	r.Use(chimw.Recoverer)
//	r.Use(radixipchi.Middleware(radixipchi.Config{
//	    Engine:    engine,
//	    Limiter:   limiter,
//	    Blocklist: true,
//	    RateLimit: true,
//	}))
//
// Because chi's middleware.RealIP already writes the resolved client IP
// into r.RemoteAddr, you can leave Config.TrustedProxies empty.
package radixipchi

import (
	nethttp "github.com/Mwangi-Derrick/radixip/lib/go/adapters/net_http"
)

// Engine is re-exported for convenience so callers need only one import.
type Engine = nethttp.Engine

// Config is re-exported from the net/http adapter.
type Config = nethttp.Config

// Middleware returns a chi middleware (func(http.Handler) http.Handler).
// It is the same value as nethttp.Middleware — no wrapping, no translation.
//
// Use it with r.Use(...) or as the sole handler wrapper:
//
//	r.Use(radixipchi.Middleware(cfg))
var Middleware = nethttp.Middleware