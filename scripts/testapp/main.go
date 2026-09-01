package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
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
	// Parse command line flags for rate limiting configuration
	var (
		burst      = flag.Uint64("burst", 1000, "Rate limiter burst size")
		refillRate = flag.Uint64("refill", 500, "Rate limiter refill rate (tokens/second)")
		ttl        = flag.Uint64("ttl", 60, "Rate limiter TTL in seconds")
		maxBuckets = flag.Uint64("max-buckets", 1000000, "Maximum number of rate limiter buckets")
		rateLimit  = flag.Bool("rate-limit", true, "Enable rate limiting")
		blocklist  = flag.Bool("blocklist", true, "Enable blocklist checking")
		port       = flag.String("port", "8081", "Main app port")
		seedPort   = flag.String("seed-port", "8082", "Seed server port")
	)
	flag.Parse()

	// Allow override via environment variables
	if envBurst := os.Getenv("RATE_LIMIT_BURST"); envBurst != "" {
		if v, err := strconv.ParseUint(envBurst, 10, 64); err == nil {
			*burst = v
		}
	}
	if envRefill := os.Getenv("RATE_LIMIT_REFILL"); envRefill != "" {
		if v, err := strconv.ParseUint(envRefill, 10, 64); err == nil {
			*refillRate = v
		}
	}

	log.Printf("🚀 Starting testapp with configuration:")
	log.Printf("   Rate Limit: %v", *rateLimit)
	log.Printf("   Blocklist: %v", *blocklist)
	log.Printf("   Burst: %d", *burst)
	log.Printf("   Refill Rate: %d tokens/sec", *refillRate) // Fixed: %d instead of %.2f
	log.Printf("   TTL: %d seconds", *ttl)
	log.Printf("   Max Buckets: %d", *maxBuckets)

	// 1. Initialize RadixIP Engine
	engine := radixip_engine.NewEngineWrapper(radixip_engine.EngineConcurrent, radixip_engine.AtomicRadixNode)
	adapter := &EngineAdapter{inner: engine}

	// 2. Initialize Rate Limiter with configurable parameters
	limiter := policy.NewTokenBucketLimiter(*burst, *refillRate, uint32(*ttl), *maxBuckets)

	// 3. Configure Middleware
	cfg := radixipgin.Config{
		Limiter:        limiter,
		Engine:         adapter,
		TrustedProxies: []string{"127.0.0.1/32", "::1/128"}, // Trust spoof proxy IP
		Blocklist:      *blocklist,
		RateLimit:      *rateLimit,
		BucketMode:     "ip", // Exact IP rate limiting
	}

	// 4. Setup Gin router
	gogin.SetMode(gogin.ReleaseMode)
	r := gogin.New()

	// Add recovery middleware to handle panics
	r.Use(gogin.Recovery())

	// Add logging middleware for debugging
	r.Use(func(c *gogin.Context) {
		// Log only if rate limit failures are high
		if strings.HasPrefix(c.Request.URL.Path, "/ping") {
			log.Printf("📊 Request from %s to %s", c.ClientIP(), c.Request.URL.Path)
		}
		c.Next()
	})

	// Apply RadixIP middleware
	r.Use(radixipgin.Middleware(cfg))

	// Simple ping route to test latency and limits
	r.GET("/ping", func(c *gogin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// Health check endpoint
	r.GET("/health", func(c *gogin.Context) {
		c.JSON(http.StatusOK, gogin.H{
			"status": "healthy",
			"config": gogin.H{
				"burst":       *burst,
				"refill":      *refillRate,
				"ttl":         *ttl,
				"max_buckets": *maxBuckets,
				"rate_limit":  *rateLimit,
				"blocklist":   *blocklist,
			},
			"blocklist_size": engine.Size(),
		})
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
		c.JSON(http.StatusOK, gogin.H{
			"seeded": added,
			"total":  engine.Size(),
		})
	})

	seedRouter.GET("/health", func(c *gogin.Context) {
		c.JSON(http.StatusOK, gogin.H{
			"status":         "healthy",
			"blocklist_size": engine.Size(),
		})
	})

	// Run seed router
	go func() {
		log.Printf("🌱 Seed server listening on :%s", *seedPort)
		log.Printf("   POST /seed - Add CIDRs to blocklist")
		log.Printf("   GET /health - Check seed server health")
		if err := seedRouter.Run(":" + *seedPort); err != nil {
			log.Fatalf("Seed server failed: %v", err)
		}
	}()

	// Run main app
	log.Printf("🚀 Main test app listening on :%s", *port)
	log.Printf("   GET /ping - Test endpoint (rate limited)")
	log.Printf("   GET /health - Check app health and config")
	if err := r.Run(":" + *port); err != nil {
		log.Fatalf("Main app failed: %v", err)
	}
}
