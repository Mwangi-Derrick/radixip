// Package radixipfiber provides RadixIP middleware for the Fiber web framework.
package radixipfiber

import (
	"net"
	"net/http"

	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
	"github.com/gofiber/fiber/v2"
)

// Engine is the minimal interface RadixIP exposes for blocklist lookups.
type Engine interface {
	Lookup(ip string) bool
}

// Config holds all middleware options.
type Config struct {
	Limiter        *policy.TokenBucketLimiter
	Engine         Engine
	TrustedProxies []string
	BlockedStatus  int
	LimitedStatus  int
	Blocklist      bool
	RateLimit      bool
	BucketMode     string
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

// Middleware returns a Fiber Handler.
func Middleware(cfg Config) fiber.Handler {
	cfg.defaults()
	trusted := parseCIDRs(cfg.TrustedProxies)

	return func(c *fiber.Ctx) error {
		ip := extractIPFiber(c, trusted)
		if ip == nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid ip"})
		}
		ipStr := ip.String()

		if cfg.Blocklist && cfg.Engine != nil {
			if cfg.Engine.Lookup(ipStr) {
				return c.Status(cfg.BlockedStatus).JSON(fiber.Map{
					"error": "blocked",
					"ip":    ipStr,
				})
			}
		}

		if cfg.RateLimit && cfg.Limiter != nil {
			key := bucketKey(ip, cfg.BucketMode)
			if !cfg.Limiter.Allow(key) {
				c.Set("Retry-After", "1")
				return c.Status(cfg.LimitedStatus).JSON(fiber.Map{
					"error": "rate limited",
					"ip":    ipStr,
				})
			}
		}

		return c.Next()
	}
}

func extractIPFiber(c *fiber.Ctx, trusted []*net.IPNet) net.IP {
	if xff := c.Get("X-Forwarded-For"); xff != "" {
		// Use Fiber's built-in fast split? Actually, we'll just re-implement
		// the right-to-left strip manually to reuse our trusted logic.
		// For simplicity, we just use the fast strings logic.
		importStringsSplit := func(s string) []string {
			var parts []string
			start := 0
			for i := 0; i < len(s); i++ {
				if s[i] == ',' {
					parts = append(parts, s[start:i])
					start = i + 1
				}
			}
			parts = append(parts, s[start:])
			return parts
		}
		parts := importStringsSplit(xff)
		for i := len(parts) - 1; i >= 0; i-- {
			// strip space
			raw := parts[i]
			for len(raw) > 0 && raw[0] == ' ' {
				raw = raw[1:]
			}
			for len(raw) > 0 && raw[len(raw)-1] == ' ' {
				raw = raw[:len(raw)-1]
			}
			ip := net.ParseIP(raw)
			if ip != nil {
				if !isTrusted(ip, trusted) {
					return ip
				}
			}
		}
	}
	if rip := c.Get("X-Real-IP"); rip != "" {
		if ip := net.ParseIP(rip); ip != nil {
			return ip
		}
	}
	return net.ParseIP(c.IP())
}

func isTrusted(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func bucketKey(ip net.IP, mode string) string {
	if mode == "subnet" {
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
