package main

import (
	"log"
	"net"
	"net/http"
	"strings"

	radixipgin "github.com/Mwangi-Derrick/radixip/lib/go/adapters/gin"
	radixip_engine "github.com/Mwangi-Derrick/radixip/lib/go/engine"
	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
	gogin "github.com/gin-gonic/gin"
)

// EngineAdapter adapts radixip_engine to the radixipgin.Engine interface.
type EngineAdapter struct {
	inner *radixip_engine.EngineWrapper
}

func (a *EngineAdapter) Lookup(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return a.inner.Lookup(ip) != nil
}

func main() {
	// 1. Initialize RadixIP Engine
	engine := radixip_engine.NewEngineWrapper(radixip_engine.EngineConcurrent, radixip_engine.AtomicRadixNode)
	adapter := &EngineAdapter{inner: engine}

	// 2. Initialize Rate Limiter: 100 max burst, 10 tokens/sec refill, 60s TTL, max 1M buckets
	limiter := policy.NewTokenBucketLimiter(100, 10, 60, 1000000)

	// 3. Configure Middleware
	cfg := radixipgin.Config{
		Limiter:        limiter,
		Engine:         adapter,
		TrustedProxies: []string{}, // No trusted proxies for this test; we spoof directly to the proxy
		Blocklist:      true,
		RateLimit:      true,
		BucketMode:     "ip", // Exact IP rate limiting
	}

	// 4. Setup Gin router
	gogin.SetMode(gogin.ReleaseMode)
	r := gogin.New()
	r.Use(radixipgin.Middleware(cfg))

	// Simple ping route to test latency and limits
	r.GET("/ping", func(c *gogin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// Unprotected endpoint to seed the blocklist
	seedRouter := gogin.New()
	seedRouter.POST("/seed", func(c *gogin.Context) {
		type SeedReq struct {
			CIDRs []string `json:"cidrs"`
		}
		var req SeedReq
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gogin.H{"error": err.Error()})
			return
		}

		added := 0
		for _, cidrStr := range req.CIDRs {
			cidrStr = strings.TrimSpace(cidrStr)
			_, netw, err := net.ParseCIDR(cidrStr)
			if err != nil {
				continue
			}
			engine.Insert(netw, radixip_engine.Metadata{Value: "blocked"})
			added++
		}
		c.JSON(http.StatusOK, gogin.H{"seeded": added, "total": engine.Size()})
	})

	// Run seed router on :8082
	go func() {
		log.Println("Seed server listening on :8082")
		log.Fatal(seedRouter.Run(":8082"))
	}()

	// Run main app on :8081
	log.Println("Main test app listening on :8081")
	log.Fatal(r.Run(":8081"))
}
