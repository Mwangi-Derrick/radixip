// Package nethttp provides RadixIP middleware for net/http-compatible routers.
//
// The middleware returned by Middleware has the standard Go middleware
// signature, func(http.Handler) http.Handler, so it composes with any router
// or middleware library built on net/http - including chi, gorilla/mux,
// httprouter, and the stdlib ServeMux.
//
// For accurate client IP extraction, mount a RealIP-style middleware before
// this one (chi users: github.com/go-chi/chi/v5/middleware.RealIP). If you
// pass Config.TrustedProxies, this package also extracts the IP itself, so
// it is safe to use standalone.
package radixipnethttp

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
)

// Engine is the minimal interface RadixIP exposes for blocklist lookups.
type Engine interface {
	Lookup(ip string) bool
}

// Config holds all middleware options.
type Config struct {
	// Limiter is the token bucket rate limiter used when RateLimit is true.
	Limiter *policy.TokenBucketLimiter

	// Engine performs blocklist lookups when Blocklist is true.
	Engine Engine

	// TrustedProxies is a list of CIDR ranges whose forwarded headers are
	// trusted. When non-empty, this middleware resolves the client IP from
	// X-Forwarded-For / X-Real-IP itself. Leave empty if an upstream
	// RealIP middleware (e.g. chi's) has already resolved it into
	// r.RemoteAddr.
	TrustedProxies []string

	// BlockedStatus is the HTTP status returned for blocklisted IPs.
	// Defaults to 403 Forbidden.
	BlockedStatus int

	// LimitedStatus is the HTTP status returned for rate-limited IPs.
	// Defaults to 429 Too Many Requests.
	LimitedStatus int

	// Blocklist enables the blocklist check.
	Blocklist bool

	// RateLimit enables the rate limit check.
	RateLimit bool

	// BucketMode determines the rate limit key granularity:
	// "ip" (default) or "subnet".
	BucketMode string
}

// defaults fills in zero-value fields with sensible defaults.
func (c *Config) defaults() {
	if c.BlockedStatus == 0 {
		c.BlockedStatus = http.StatusForbidden
	}
	if c.LimitedStatus == 0 {
		c.LimitedStatus = http.StatusTooManyRequests
	}
	if c.BucketMode == "" {
		c.BucketMode = "ip"
	}
}

// Middleware returns a net/http middleware that enforces the configured
// blocklist and rate-limit policies. The signature matches chi's middleware
// and the wider net/http ecosystem, so the same value can be re-exported by
// router-specific packages (e.g. adapters/chi).
//
// Typical wiring:
//
//	mw := radixipnethttp.Middleware(radixipnethttp.Config{
//	    Engine:         engine,
//	    Limiter:        limiter,
//	    TrustedProxies: []string{"10.0.0.0/8"},
//	    Blocklist:      true,
//	    RateLimit:      true,
//	})
//	http.Handle("/", mw(yourHandler))
func Middleware(cfg Config) func(http.Handler) http.Handler {
	cfg.defaults()
	trusted := parseCIDRs(cfg.TrustedProxies)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Resolve the client IP. If TrustedProxies is empty and an
			// upstream RealIP middleware already ran, this falls back to
			// parsing r.RemoteAddr.
			ip := clientIP(r, trusted)
			if ip == nil {
				writeJSON(w, http.StatusBadRequest,
					map[string]string{"error": "invalid ip"})
				return
			}
			ipStr := ip.String()

			// Blocklist check: reject known-bad IPs.
			if cfg.Blocklist && cfg.Engine != nil {
				if cfg.Engine.Lookup(ipStr) {
					writeJSON(w, cfg.BlockedStatus, map[string]string{
						"error": "blocked",
						"ip":    ipStr,
					})
					return
				}
			}

			// Rate limit check: reject IPs that have exhausted their bucket.
			if cfg.RateLimit && cfg.Limiter != nil {
				key := bucketKey(ip, cfg.BucketMode)
				if !cfg.Limiter.Allow(key) {
					w.Header().Set("Retry-After", "1")
					writeJSON(w, cfg.LimitedStatus, map[string]string{
						"error": "rate limited",
						"ip":    ipStr,
					})
					return
				}
			}

			// All checks passed. In net/http there is no c.Next() — simply
			// calling next.ServeHTTP is what continues the chain.
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP resolves the client IP for a request. It first trusts whatever
// upstream RealIP middleware has written into r.RemoteAddr, then falls back
// to walking X-Forwarded-For right-to-left (skipping trusted proxies), and
// finally X-Real-IP.
func clientIP(r *http.Request, trusted []*net.IPNet) net.IP {
	// If r.RemoteAddr parses as host:port, prefer it. An upstream RealIP
	// middleware would have overwritten it with the resolved client IP.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip
		}
	}

	// Fallback: walk X-Forwarded-For right-to-left, skipping trusted hops.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := splitComma(xff)
		for i := len(parts) - 1; i >= 0; i-- {
			raw := trimSpace(parts[i])
			if ip := net.ParseIP(raw); ip != nil && !isTrusted(ip, trusted) {
				return ip
			}
		}
	}

	// Last resort: X-Real-IP.
	if rip := r.Header.Get("X-Real-IP"); rip != "" {
		if ip := net.ParseIP(rip); ip != nil {
			return ip
		}
	}

	// Nothing worked — try the raw RemoteAddr one more time (no port).
	return net.ParseIP(r.RemoteAddr)
}

// isTrusted reports whether ip falls within any of the trusted CIDR ranges.
func isTrusted(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// bucketKey returns the rate limit key for an IP based on the configured mode.
// In "subnet" mode, IPv4 addresses are grouped by /24 and IPv6 by /48.
func bucketKey(ip net.IP, mode string) string {
	if mode == "subnet" {
		if ip.To4() != nil {
			if _, net24, err := net.ParseCIDR(ip.String() + "/24"); err == nil {
				return net24.String()
			}
		} else {
			if _, net48, err := net.ParseCIDR(ip.String() + "/48"); err == nil {
				return net48.String()
			}
		}
	}
	return ip.String()
}

// parseCIDRs parses a slice of CIDR strings into *net.IPNet values, silently
// skipping entries that fail to parse.
func parseCIDRs(cidrs []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, s := range cidrs {
		if _, n, err := net.ParseCIDR(s); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}

// writeJSON emits a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// splitComma splits s on commas without allocating via strings.Split.
func splitComma(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// trimSpace removes leading and trailing ASCII spaces from s.
func trimSpace(s string) string {
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}