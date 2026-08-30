// Spoof Proxy — injects random fake IPs into X-Forwarded-For before
// forwarding to the application under test.
//
// Usage:
//
//	go run ./scripts/spoof_proxy \
//	  --listen  :8080 \
//	  --upstream http://localhost:8081 \
//	  --subnets  198.51.100.0/24,203.0.113.0/24,192.0.2.0/24
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	rng     = rand.New(rand.NewSource(time.Now().UnixNano()))
	rngLock sync.Mutex
)

// Pre-compute random IP pools for each CIDR to avoid parsing on every request
type IPPool struct {
	cidr   string
	ips    []net.IP
	mu     sync.RWMutex
	cached bool
}

func (p *IPPool) getRandomIP() string {
	p.mu.RLock()
	if p.cached {
		ip := p.ips[rng.Intn(len(p.ips))]
		p.mu.RUnlock()
		return ip.String()
	}
	p.mu.RUnlock()

	// Build the pool
	p.mu.Lock()
	defer p.mu.Unlock()
	// Double-check after acquiring write lock
	if p.cached {
		ip := p.ips[rng.Intn(len(p.ips))]
		return ip.String()
	}

	_, network, err := net.ParseCIDR(p.cidr)
	if err != nil {
		log.Printf("Failed to parse CIDR %s: %v", p.cidr, err)
		return "198.51.100.1"
	}

	ip := network.IP.To4()
	if ip == nil {
		log.Printf("CIDR %s is not IPv4", p.cidr)
		return "198.51.100.1"
	}

	ones, bits := network.Mask.Size()
	hostBits := bits - ones
	if hostBits <= 0 {
		p.ips = []net.IP{ip}
		p.cached = true
		return ip.String()
	}

	// Pre-compute all possible IPs in the subnet (capped at 10000 to avoid memory issues)
	maxIPs := 1 << hostBits
	capped := false
	if maxIPs > 10000 {
		maxIPs = 10000
		capped = true
	}

	p.ips = make([]net.IP, 0, maxIPs)
	base := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	totalIPs := 1 << hostBits

	for i := 1; i < maxIPs && i < totalIPs-1; i++ {
		addr := base + uint32(i)
		newIP := net.IPv4(
			byte((addr>>24)&0xFF),
			byte((addr>>16)&0xFF),
			byte((addr>>8)&0xFF),
			byte(addr&0xFF),
		)
		p.ips = append(p.ips, newIP)
	}

	// Always include the network address and broadcast address for variety
	if len(p.ips) == 0 {
		p.ips = []net.IP{ip}
	} else if len(p.ips) < 3 && totalIPs > 3 {
		// Add some random IPs from the subnet
		for i := 0; i < 5 && len(p.ips) < 10; i++ {
			addr := base + uint32(rng.Intn(totalIPs-2)+1)
			newIP := net.IPv4(
				byte((addr>>24)&0xFF),
				byte((addr>>16)&0xFF),
				byte((addr>>8)&0xFF),
				byte(addr&0xFF),
			)
			p.ips = append(p.ips, newIP)
		}
	}

	p.cached = true
	if capped {
		log.Printf("Capped subnet %s to %d IPs", p.cidr, maxIPs)
	}

	selected := p.ips[rng.Intn(len(p.ips))]
	return selected.String()
}

func main() {
	listen := flag.String("listen", ":8080", "address to listen on")
	upstream := flag.String("upstream", "http://localhost:8081", "upstream application URL")
	subnetsFlag := flag.String("subnets",
		"198.51.100.0/24,203.0.113.0/24,192.0.2.0/24,10.20.30.0/24,172.16.50.0/24",
		"comma-separated list of CIDRs to sample fake IPs from")
	workers := flag.Int("workers", 100, "number of worker goroutines for upstream connections")
	maxIdle := flag.Int("max-idle", 100, "max idle connections per host")
	timeout := flag.Duration("timeout", 5*time.Second, "upstream request timeout")
	quiet := flag.Bool("quiet", false, "disable request logging")
	flag.Parse()

	target, err := url.Parse(*upstream)
	if err != nil {
		log.Fatalf("invalid upstream URL: %v", err)
	}

	subnets := strings.Split(*subnetsFlag, ",")

	// Create IP pools for each subnet
	pools := make([]*IPPool, len(subnets))
	for i, cidr := range subnets {
		pools[i] = &IPPool{cidr: strings.TrimSpace(cidr)}
	}

	// Create reverse proxy with connection pooling
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Optimized transport with worker pool support
	transport := &http.Transport{
		MaxIdleConns:          *maxIdle * 2,
		MaxIdleConnsPerHost:   *maxIdle,
		MaxConnsPerHost:       *maxIdle,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true,
		DisableKeepAlives:     false,
		ResponseHeaderTimeout: *timeout,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   2 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	proxy.Transport = transport

	// Fast IP selection using atomic increment
	var poolIndex uint32

	proxy.Director = func(r *http.Request) {
		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.Host = target.Host

		// Fast random IP selection using atomic increment
		idx := int(atomic.AddUint32(&poolIndex, 1) % uint32(len(pools)))
		pool := pools[idx]

		fakeIP := pool.getRandomIP()
		r.Header.Set("X-Forwarded-For", fakeIP)
		r.Header.Set("X-Real-IP", fakeIP)
	}

	// Error handler
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if !*quiet {
			log.Printf("Proxy error: %v", err)
		}
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	// Health check endpoint (bypasses proxy)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy","pools":` + fmt.Sprintf("%d", len(pools)) + `}`))
	})

	// Metrics endpoint
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","type":"spoof-proxy"}`))
	})

	// Main handler with logging
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !*quiet && r.URL.Path != "/health" && r.URL.Path != "/metrics" {
			log.Printf("➡️ %s %s (X-Forwarded-For: %s)",
				r.Method, r.URL.Path, r.Header.Get("X-Forwarded-For"))
		}
		proxy.ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:         *listen,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("🚀 Spoof proxy listening on %s → %s", *listen, *upstream)
	log.Printf("   Workers: %d, MaxIdle: %d, Timeout: %v", *workers, *maxIdle, *timeout)
	log.Printf("   Subnets: %d pools", len(pools))
	log.Printf("   GET /health - Health check")
	log.Printf("   GET /metrics - Metrics endpoint")

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	sig := <-sigChan
	log.Printf("Received signal %v, shutting down gracefully...", sig)

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown the server
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
		os.Exit(1)
	}

	log.Println("Server stopped gracefully")
}
