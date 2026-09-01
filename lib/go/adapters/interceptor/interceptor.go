package radixipgrpc

// Package radixipgrpc provides RadixIP interceptors for gRPC.
//
// # Usage
//
//	cfg := radixipgrpc.Config{
//	    Limiter:         myLimiter,          // *policy.TokenBucketLimiter
//	    Engine:          myRadixEngine,       // radixip.Engine
//	    TrustedProxies:  []string{"10.0.0.0/8"},
//	    BlockedStatus:   403,
//	    LimitedStatus:   429,
//	}
//
//	s := grpc.NewServer(
//	    grpc.UnaryInterceptor(radixipgrpc.UnaryInterceptor(cfg)),
//	    grpc.StreamInterceptor(radixipgrpc.StreamInterceptor(cfg)),
//	)

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Engine is the minimal interface RadixIP exposes for blocklist lookups.
type Engine interface {
	Lookup(ip string) bool // returns true if the IP is in the blocklist
}

// Config holds all interceptor options.
type Config struct {
	// Limiter is the token bucket rate limiter. Required when RateLimit is true.
	Limiter *policy.TokenBucketLimiter
	// Engine is the RadixIP blocklist engine. Required when Blocklist is true.
	Engine Engine
	// TrustedProxies are CIDRs whose IPs are stripped from the XFF header.
	TrustedProxies []string
	// BlockedStatus is the HTTP status returned for blocklist hits (default 403).
	// Maps to gRPC codes.PermissionDenied.
	BlockedStatus int
	// LimitedStatus is the HTTP status returned for rate-limit hits (default 429).
	// Maps to gRPC codes.ResourceExhausted.
	LimitedStatus int
	// Blocklist enables the RadixIP LPM blocklist check.
	Blocklist bool
	// RateLimit enables the token bucket rate limit check.
	RateLimit bool
	// BucketMode: "ip" or "subnet" (default "ip")
	BucketMode string
	// MetadataPrefix is the prefix for gRPC metadata keys (default "radixip")
	MetadataPrefix string
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
	if c.MetadataPrefix == "" {
		c.MetadataPrefix = "radixip"
	}
}

// statusCodeToGRPC maps HTTP status codes to gRPC status codes.
func statusCodeToGRPC(httpStatus int) codes.Code {
	switch httpStatus {
	case http.StatusForbidden:
		return codes.PermissionDenied
	case http.StatusTooManyRequests:
		return codes.ResourceExhausted
	case http.StatusBadRequest:
		return codes.InvalidArgument
	default:
		return codes.Unknown
	}
}

// UnaryInterceptor returns a gRPC unary interceptor that:
//  1. Extracts the real client IP (metadata headers → RemoteAddr).
//  2. Checks the blocklist (if enabled) — returns PermissionDenied on hit.
//  3. Checks the token bucket (if enabled) — returns ResourceExhausted on exhaustion.
//  4. Calls the handler otherwise.
func UnaryInterceptor(cfg Config) grpc.UnaryServerInterceptor {
	cfg.defaults()

	trusted := parseCIDRs(cfg.TrustedProxies)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ip, err := extractIPFromContext(ctx, trusted)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to extract IP: %v", err)
		}

		ipStr := ip.String()

		// 1. Blocklist check.
		if cfg.Blocklist && cfg.Engine != nil {
			if cfg.Engine.Lookup(ipStr) {
				return nil, status.Errorf(
					statusCodeToGRPC(cfg.BlockedStatus),
					"blocked: IP %s is in blocklist",
					ipStr,
				)
			}
		}

		// 2. Rate limit check.
		if cfg.RateLimit && cfg.Limiter != nil {
			key := bucketKey(ip, cfg.BucketMode)
			if !cfg.Limiter.Allow(key) {
				// Add retry-after to response metadata
				if err := grpc.SetHeader(ctx, metadata.Pairs("retry-after", "1")); err != nil {
					// Log but continue
				}
				return nil, status.Errorf(
					statusCodeToGRPC(cfg.LimitedStatus),
					"rate limited: IP %s exceeded rate limit",
					ipStr,
				)
			}
		}

		return handler(ctx, req)
	}
}

// StreamInterceptor returns a gRPC stream interceptor with the same logic as UnaryInterceptor.
func StreamInterceptor(cfg Config) grpc.StreamServerInterceptor {
	cfg.defaults()

	trusted := parseCIDRs(cfg.TrustedProxies)

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		ip, err := extractIPFromContext(ctx, trusted)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "failed to extract IP: %v", err)
		}

		ipStr := ip.String()

		// 1. Blocklist check.
		if cfg.Blocklist && cfg.Engine != nil {
			if cfg.Engine.Lookup(ipStr) {
				return status.Errorf(
					statusCodeToGRPC(cfg.BlockedStatus),
					"blocked: IP %s is in blocklist",
					ipStr,
				)
			}
		}

		// 2. Rate limit check.
		if cfg.RateLimit && cfg.Limiter != nil {
			key := bucketKey(ip, cfg.BucketMode)
			if !cfg.Limiter.Allow(key) {
				// Add retry-after to response metadata
				if err := grpc.SetHeader(ctx, metadata.Pairs("retry-after", "1")); err != nil {
					// Log but continue
				}
				return status.Errorf(
					statusCodeToGRPC(cfg.LimitedStatus),
					"rate limited: IP %s exceeded rate limit",
					ipStr,
				)
			}
		}

		return handler(srv, ss)
	}
}

// extractIPFromContext extracts the client IP from gRPC context.
// It checks in order: metadata headers (X-Forwarded-For, X-Real-IP), then peer address.
func extractIPFromContext(ctx context.Context, trusted []*net.IPNet) (net.IP, error) {
	// Try to get IP from metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		// Check for X-Forwarded-For
		if xff := md.Get("x-forwarded-for"); len(xff) > 0 {
			if ip := parseIPFromXFF(xff[0], trusted); ip != nil {
				return ip, nil
			}
		}
		// Check for X-Real-IP
		if xri := md.Get("x-real-ip"); len(xri) > 0 {
			if ip := net.ParseIP(xri[0]); ip != nil && !isTrustedProxy(ip, trusted) {
				return ip, nil
			}
		}
	}

	// Fallback to peer address (similar to RemoteAddr for HTTP)
	peer, _ := peer.FromContext(ctx)
	if peer != nil && peer.Addr != nil {
		host, _, err := net.SplitHostPort(peer.Addr.String())
		if err == nil {
			if ip := net.ParseIP(host); ip != nil {
				return ip, nil
			}
		}
	}

	return nil, status.Errorf(codes.InvalidArgument, "could not extract client IP")
}

// parseIPFromXFF parses the X-Forwarded-For header, stripping trusted proxies.
func parseIPFromXFF(xff string, trusted []*net.IPNet) net.IP {
	ips := parseXFF(xff)
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		// Skip trusted proxies
		if isTrustedProxy(ip, trusted) {
			continue
		}
		return ip
	}
	return nil
}

// parseXFF splits the X-Forwarded-For header by comma.
func parseXFF(xff string) []string {
	// Simple split; production code might need more robust parsing
	var ips []string
	for _, part := range strings.Split(xff, ",") {
		ips = append(ips, strings.TrimSpace(part))
	}
	return ips
}

// isTrustedProxy checks if an IP is in the trusted proxy list.
func isTrustedProxy(ip net.IP, trusted []*net.IPNet) bool {
	for _, net := range trusted {
		if net.Contains(ip) {
			return true
		}
	}
	return false
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
