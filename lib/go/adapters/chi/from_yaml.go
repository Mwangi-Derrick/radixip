package radixipchi

import (
	nethttp "github.com/Mwangi-Derrick/radixip/lib/go/adapters/net_http"
)

// NewFromYAML creates a hot-reloading handler from a YAML config file.
//
// It is re-exported from the net/http adapter and returns a terminal
// http.Handler, not a chi middleware. In a chi stack you almost always
// want MiddlewareFromYAML instead, which returns
// func(http.Handler) http.Handler and slots into r.Use(...).
//
// Use NewFromYAML only when you want a self-contained handler to mount
// at a specific route, e.g.:
//
//	h, stop, err := radixipchi.NewFromYAML("radixip.yaml", engine)
//	if err != nil { log.Fatal(err) }
//	defer stop()
//	r.Handle("/api/*", h)
var NewFromYAML = nethttp.NewFromYAML

// MiddlewareFromYAML creates a hot-reloading chi middleware from a YAML
// config file.
//
// The engine adapter is passed separately because the ART tree state
// (blocklist data) is managed outside the config lifecycle. Only
// rate-limit parameters and middleware options are hot-swapped when the
// file changes.
//
// stop() must be called on server shutdown to release fsnotify resources.
//
// Typical wiring:
//
//	mw, stop, err := radixipchi.NewFromYAML("radixip.yaml", engine)
//	if err != nil { log.Fatal(err) }
//	defer stop()
//	r.Use(mw)
var MiddlewareFromYAML = nethttp.NewFromYAML