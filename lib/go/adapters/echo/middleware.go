// Package radixipecho provides RadixIP middleware for the Echo web framework.
package radixipecho

import (
	"net"
	"net/http"

	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
	"github.com/labstack/echo/v4"
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

// Middleware returns an Echo MiddlewareFunc.
func Middleware(cfg Config) echo.MiddlewareFunc {
	cfg.defaults()
	trusted := parseCIDRs(cfg.TrustedProxies)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ip, err := policy.ExtractIP(c.Request(), trusted)
			if err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid ip"})
			}
			ipStr := ip.String()

			if cfg.Blocklist && cfg.Engine != nil {
				if cfg.Engine.Lookup(ipStr) {
					return c.JSON(cfg.BlockedStatus, map[string]string{
						"error": "blocked",
						"ip":    ipStr,
					})
				}
			}

			if cfg.RateLimit && cfg.Limiter != nil {
				key := bucketKey(ip, cfg.BucketMode)
				if !cfg.Limiter.Allow(key) {
					c.Response().Header().Set("Retry-After", "1")
					return c.JSON(cfg.LimitedStatus, map[string]string{
						"error": "rate limited",
						"ip":    ipStr,
					})
				}
			}

			return next(c)
		}
	}
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
