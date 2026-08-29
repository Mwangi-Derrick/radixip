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
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

var rng = rand.New(rand.NewSource(42))

func randomIPFromCIDR(cidr string) (string, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", err
	}
	ip := network.IP.To4()
	if ip == nil {
		return "", fmt.Errorf("only IPv4 CIDRs supported in spoof proxy")
	}
	ones, bits := network.Mask.Size()
	hostBits := bits - ones
	if hostBits <= 0 {
		return network.IP.String(), nil
	}
	offset := uint32(rng.Intn(1<<hostBits-2) + 1)
	base := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	addr := base + offset
	return fmt.Sprintf("%d.%d.%d.%d",
		(addr>>24)&0xFF, (addr>>16)&0xFF, (addr>>8)&0xFF, addr&0xFF), nil
}

func main() {
	listen := flag.String("listen", ":8080", "address to listen on")
	upstream := flag.String("upstream", "http://localhost:8081", "upstream application URL")
	subnetsFlag := flag.String("subnets",
		"198.51.100.0/24,203.0.113.0/24,192.0.2.0/24,10.20.30.0/24,172.16.50.0/24",
		"comma-separated list of CIDRs to sample fake IPs from")
	flag.Parse()

	target, err := url.Parse(*upstream)
	if err != nil {
		log.Fatalf("invalid upstream URL: %v", err)
	}
	subnets := strings.Split(*subnetsFlag, ",")

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(r *http.Request) {
		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.Host = target.Host

		// Pick a random subnet, then a random IP within it.
		cidr := subnets[rng.Intn(len(subnets))]
		fakeIP, err := randomIPFromCIDR(cidr)
		if err != nil {
			fakeIP = "198.51.100.1"
		}
		r.Header.Set("X-Forwarded-For", fakeIP)
		r.Header.Set("X-Real-IP", fakeIP)
	}

	log.Printf("Spoof proxy listening on %s → %s", *listen, *upstream)
	log.Fatal(http.ListenAndServe(*listen, proxy))
}
