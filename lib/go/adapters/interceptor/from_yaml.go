package radixipgrpc

// from_yaml.go — hot-reload integration for gRPC via fsnotify.
//
// Usage:
//
//	interceptor, stop, err := radixipgrpc.NewFromYAML("radixip.yaml", engineAdapter)
//	if err != nil { log.Fatal(err) }
//	defer stop()
//	s := grpc.NewServer(
//	    grpc.UnaryInterceptor(interceptor),
//	    grpc.StreamInterceptor(interceptor),
//	)

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/Mwangi-Derrick/radixip/lib/go/config"
	"github.com/Mwangi-Derrick/radixip/lib/go/policy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// yamlState bundles a config snapshot with the limiter derived from it.
type yamlState struct {
	cfg     *config.RadixIpConfig
	limiter *policy.TokenBucketLimiter
}

func newYAMLState(cfg *config.RadixIpConfig) *yamlState {
	rl := cfg.RadixIP.RateLimit
	lim := policy.NewTokenBucketLimiter(
		rl.Capacity,
		rl.RefillRate,
		rl.TTLSeconds,
		rl.MaxBuckets,
	)
	log.Printf("radixipgrpc: (re)built limiter capacity=%d refill=%d/s", rl.Capacity, rl.RefillRate)
	return &yamlState{cfg: cfg, limiter: lim}
}

// grpcWatcher holds the atomic hot-swap state.
type grpcWatcher struct {
	watcher *config.Watcher
	state   atomic.Pointer[yamlState]
	engine  Engine
}

// UnaryInterceptor implements the gRPC unary interceptor for the watcher.
func (g *grpcWatcher) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		latest := g.watcher.Current()
		s := g.state.Load()

		// Config pointer changed → rebuild limiter.
		if s.cfg != latest {
			next := newYAMLState(latest)
			g.state.Store(next)
			s = next
		}

		mwCfg := latest.RadixIP.Middleware
		trusted := parseCIDRs(mwCfg.TrustedProxies)

		ip, err := extractIPFromContext(ctx, trusted)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to extract IP: %v", err)
		}
		ipStr := ip.String()

		// Blocklist check.
		if latest.RadixIP.Blocklist.Enabled && g.engine != nil {
			if g.engine.Lookup(ipStr) {
				return nil, status.Errorf(
					statusCodeToGRPC(mwCfg.Responses.Blocked),
					"blocked: IP %s is in blocklist",
					ipStr,
				)
			}
		}

		// Rate limit check.
		if latest.RadixIP.RateLimit.Enabled && s.limiter != nil {
			key := bucketKey(ip, latest.RadixIP.RateLimit.BucketMode.Mode)
			if !s.limiter.Allow(key) {
				// Add retry-after to response metadata
				if err := grpc.SetHeader(ctx, metadata.Pairs("retry-after", "1")); err != nil {
					// Log but continue
				}
				return nil, status.Errorf(
					statusCodeToGRPC(mwCfg.Responses.RateLimited),
					"rate limited: IP %s exceeded rate limit",
					ipStr,
				)
			}
		}

		return handler(ctx, req)
	}
}

// StreamInterceptor implements the gRPC stream interceptor for the watcher.
func (g *grpcWatcher) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		latest := g.watcher.Current()
		s := g.state.Load()

		// Config pointer changed → rebuild limiter.
		if s.cfg != latest {
			next := newYAMLState(latest)
			g.state.Store(next)
			s = next
		}

		mwCfg := latest.RadixIP.Middleware
		trusted := parseCIDRs(mwCfg.TrustedProxies)

		ip, err := extractIPFromContext(ctx, trusted)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "failed to extract IP: %v", err)
		}
		ipStr := ip.String()

		// Blocklist check.
		if latest.RadixIP.Blocklist.Enabled && g.engine != nil {
			if g.engine.Lookup(ipStr) {
				return status.Errorf(
					statusCodeToGRPC(mwCfg.Responses.Blocked),
					"blocked: IP %s is in blocklist",
					ipStr,
				)
			}
		}

		// Rate limit check.
		if latest.RadixIP.RateLimit.Enabled && s.limiter != nil {
			key := bucketKey(ip, latest.RadixIP.RateLimit.BucketMode.Mode)
			if !s.limiter.Allow(key) {
				// Add retry-after to response metadata
				if err := grpc.SetHeader(ctx, metadata.Pairs("retry-after", "1")); err != nil {
					// Log but continue
				}
				return status.Errorf(
					statusCodeToGRPC(mwCfg.Responses.RateLimited),
					"rate limited: IP %s exceeded rate limit",
					ipStr,
				)
			}
		}

		return handler(srv, ss)
	}
}

// NewFromYAML creates a hot-reloading gRPC interceptor from a YAML config file.
//
// The engine adapter is passed separately because the ART tree state (blocklist
// data) is managed outside the config lifecycle. Only rate-limit parameters and
// interceptor options are hot-swapped when the file changes.
//
// stop() must be called on server shutdown to release fsnotify resources.
//
// Returns a unary interceptor, stream interceptor, and a stop function.
func NewFromYAML(path string, engine Engine) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor, func(), error) {
	w, err := config.NewWatcher(path)
	if err != nil {
		return nil, nil, nil, err
	}

	gw := &grpcWatcher{watcher: w, engine: engine}
	gw.state.Store(newYAMLState(w.Current()))

	return gw.UnaryInterceptor(), gw.StreamInterceptor(), w.Stop, nil
}

// Alternative: Combined interceptor that returns a single interceptor that works
// for both unary and stream methods. This is useful if you want to use the same
// interceptor for both types.
type CombinedInterceptor interface {
	UnaryInterceptor() grpc.UnaryServerInterceptor
	StreamInterceptor() grpc.StreamServerInterceptor
}

// NewCombinedFromYAML returns a CombinedInterceptor interface that provides
// both unary and stream interceptors from the same YAML config.
func NewCombinedFromYAML(path string, engine Engine) (CombinedInterceptor, func(), error) {
	w, err := config.NewWatcher(path)
	if err != nil {
		return nil, nil, err
	}

	gw := &grpcWatcher{watcher: w, engine: engine}
	gw.state.Store(newYAMLState(w.Current()))

	return gw, w.Stop, nil
}

// // Helper function to convert HTTP status to gRPC status code.
// func statusCodeToGRPC(httpStatus int) codes.Code {
// 	switch httpStatus {
// 	case 403:
// 		return codes.PermissionDenied
// 	case 429:
// 		return codes.ResourceExhausted
// 	case 400:
// 		return codes.InvalidArgument
// 	default:
// 		return codes.Unknown
// 	}
// }
