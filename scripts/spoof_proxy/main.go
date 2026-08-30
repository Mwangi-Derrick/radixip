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
	"strings"
	"sync"
	"time"
)

var (
	rng     = rand.New(rand.NewSource(time.Now().UnixNano()))
	rngLock sync.Mutex
)

func randomIPFromCIDR(cidr string) string {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return "198.51.100.1"
	}

	ip := network.IP.To4()
	if ip == nil {
		return "198.51.100.1"
	}

	ones, bits := network.Mask.Size()
	hostBits := bits - ones
	if hostBits <= 0 {
		return network.IP.String()
	}

	rngLock.Lock()
	offset := uint32(rng.Intn(1<<hostBits-2) + 1)
	rngLock.Unlock()

	base := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	addr := base + offset

	return fmt.Sprintf("%d.%d.%d.%d",
		(addr>>24)&0xFF, (addr>>16)&0xFF, (addr>>8)&0xFF, addr&0xFF)
}

func main() {
	listen := flag.String("listen", ":8080", "address to listen on")
	upstream := flag.String("upstream", "http://localhost:8081", "upstream URL")
	subnetsFlag := flag.String("subnets",
		"198.51.100.0/24,203.0.113.0/24,192.0.2.0/24,10.20.30.0/24",
		"comma-separated CIDRs")
	flag.Parse()

	target, _ := url.Parse(*upstream)
	subnets := strings.Split(*subnetsFlag, ",")

	// Create proxy with robust transport
	proxy := httputil.NewSingleHostReverseProxy(target)

	proxy.Transport = &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 200,
		MaxConnsPerHost:     200,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 5 * time.Second,
	}

	proxy.Director = func(r *http.Request) {
		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.Host = target.Host

		cidr := subnets[rand.Intn(len(subnets))]
		fakeIP := randomIPFromCIDR(cidr)
		r.Header.Set("X-Forwarded-For", fakeIP)
		r.Header.Set("X-Real-IP", fakeIP)
	}

	// Handle proxy errors gracefully
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		// Don't log every error to avoid spam
		if strings.Contains(err.Error(), "context canceled") {
			return
		}
		log.Printf("Proxy error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	// Health check
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","subnets":%d}`, len(subnets))
	})

	// Main handler with timeout protection
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			return
		}

		// Create a context with timeout
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		r = r.WithContext(ctx)

		proxy.ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:         *listen,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("🚀 Spoof proxy listening on %s → %s", *listen, *upstream)
	log.Fatal(server.ListenAndServe())
}
