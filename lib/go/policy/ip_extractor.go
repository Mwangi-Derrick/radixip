// Package policy — IP extraction from HTTP headers.
package policy

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ExtractIP extracts the real client IP from an http.Request, stripping
// trusted proxy hops from the X-Forwarded-For chain.
//
//   - trustedProxies: CIDRs whose IPs are considered proxy hops, not clients.
func ExtractIP(r *http.Request, trustedProxies []*net.IPNet) (net.IP, error) {
	// 1. Try X-Forwarded-For — walk right-to-left, strip trusted hops.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			raw := strings.TrimSpace(parts[i])
			ip := net.ParseIP(raw)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP in X-Forwarded-For: %q", raw)
			}
			if !isTrusted(ip, trustedProxies) {
				return ip, nil
			}
		}
	}

	// 2. X-Real-IP fallback.
	if rip := r.Header.Get("X-Real-IP"); rip != "" {
		ip := net.ParseIP(strings.TrimSpace(rip))
		if ip == nil {
			return nil, fmt.Errorf("invalid X-Real-IP: %q", rip)
		}
		return ip, nil
	}

	// 3. Raw socket address fallback.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("invalid RemoteAddr: %q", r.RemoteAddr)
	}
	return ip, nil
}

func isTrusted(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
