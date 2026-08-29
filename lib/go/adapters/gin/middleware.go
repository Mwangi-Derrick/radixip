// Package radixipgin provides RadixIP middleware for the Gin web framework.
//
// # Usage
//
//	cfg := radixipgin.Config{
//	    Limiter:         myLimiter,          // *policy.TokenBucketLimiter
//	    Engine:          myRadixEngine,       // radixip.Engine
//	    TrustedProxies:  []string{"10.0.0.0/8"},
//	    BlockedStatus:   403,
//	    LimitedStatus:   429,
//	}
//
//	r := gin.Default()
//	r.Use(radixipgin.Middleware(cfg))
package radixipgin

import (
	"net"
	"net/http"

	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
	"github.com/gin-gonic/gin"
)

// Engine is the minimal interface RadixIP exposes for blocklist lookups.
type Engine interface {
	Lookup(ip string) bool // returns true if the IP is in the blocklist
}

// Config holds all middleware options.
type Config struct {
	// Limiter is the token bucket rate limiter. Required when RateLimit is true.
	Limiter *policy.TokenBucketLimiter
	// Engine is the RadixIP blocklist engine. Required when Blocklist is true.
	Engine Engine
	// TrustedProxies are CIDRs whose IPs are stripped from the XFF header.
	TrustedProxies []string
	// BlockedStatus is the HTTP status returned for blocklist hits (default 403).
	BlockedStatus int
	// LimitedStatus is the HTTP status returned for rate-limit hits (default 429).
	LimitedStatus int
	// Blocklist enables the RadixIP LPM blocklist check.
	Blocklist bool
	// RateLimit enables the token bucket rate limit check.
	RateLimit bool
	// BucketMode: "ip" or "subnet" (default "ip")
	BucketMode string
}

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

// Middleware returns a Gin HandlerFunc that:
//  1. Extracts the real client IP (XFF → X-Real-IP → RemoteAddr).
//  2. Checks the blocklist (if enabled) — returns 403 on hit.
//  3. Checks the token bucket (if enabled) — returns 429 on exhaustion.
//  4. Calls c.Next() otherwise.
func Middleware(cfg Config) gin.HandlerFunc {
	cfg.defaults()

	trusted := parseCIDRs(cfg.TrustedProxies)

	return func(c *gin.Context) {
		ip, err := policy.ExtractIP(c.Request, trusted)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ipStr := ip.String()

		// 1. Blocklist check.
		if cfg.Blocklist && cfg.Engine != nil {
			if cfg.Engine.Lookup(ipStr) {
				c.AbortWithStatusJSON(cfg.BlockedStatus, gin.H{
					"error": "blocked",
					"ip":    ipStr,
				})
				return
			}
		}

		// 2. Rate limit check.
		if cfg.RateLimit && cfg.Limiter != nil {
			key := bucketKey(ip, cfg.BucketMode)
			if !cfg.Limiter.Allow(key) {
				c.Header("Retry-After", "1")
				c.AbortWithStatusJSON(cfg.LimitedStatus, gin.H{
					"error": "rate limited",
					"ip":    ipStr,
				})
				return
			}
		}

		c.Next()
	}
}

// bucketKey returns the rate-limit bucket key for the given IP.
func bucketKey(ip net.IP, mode string) string {
	if mode == "subnet" {
		// Use the /24 (IPv4) or /48 (IPv6) prefix as the key.
		if ip.To4() != nil {
			_, net24, _ := net.ParseCIDR(ip.String() + "/24")
			if net24 != nil {
				return net24.String()
			}
		} else {
			_, net48, _ := net.ParseCIDR(ip.String() + "/48")
			if net48 != nil {
				return net48.String()
			}
		}
	}
	return ip.String()
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, s := range cidrs {
		_, n, err := net.ParseCIDR(s)
		if err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}
