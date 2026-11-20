# RadixIP chi Middleware

RadixIP middleware for [chi](https://github.com/go-chi/chi), with blocklist checks, configurable rate limiting, trusted-proxy IP extraction, route policies, auto-ban, and YAML hot reload.

chi is built directly on `net/http`, so this package is a thin re-export of the [net/http adapter](../nethttp). The middleware signature is identical and composes with chi's own middleware stack (`github.com/go-chi/chi/v5/middleware`).

## Install

```bash
go get github.com/Mwangi-Derrick/radixip/lib/go/adapters/chi
```

## Hot-reloaded configuration

```go
mw, stop, err := radixipchi.MiddlewareFromYAML("radixip.yaml", engineAdapter)
if err != nil {
    log.Fatal(err)
}
defer stop()

r := chi.NewRouter()
r.Use(middleware.RealIP)
r.Use(mw)
```

`engineAdapter` implements `Lookup(string) bool` for blocklist checks. The middleware reads `X-Forwarded-For`, `X-Real-IP`, or the peer address, applies the shared YAML policy, and returns the configured blocked or rate-limited status.

Because chi's `middleware.RealIP` already writes the resolved client IP into `r.RemoteAddr`, you can leave `Config.TrustedProxies` empty. Populate it only if you want the RadixIP middleware to resolve the client IP itself.

## Rate-limit YAML

chi uses the net/http adapter's shared YAML loader. See the runnable
[algorithm example](../../../../config/examples/rate-limit-algorithms.yaml)
and [field guide](../../../../docs/rate-limiting.md). `algorithm` chooses the
limiter, `capacity` sets its allowance, and `window_seconds` applies to the
fixed and sliding window algorithms.
For engine, proxy, blocklist, XDP, auto-ban, and metrics fields, see the
[full configuration guide](../../../../docs/configuration.md).

## Static configuration

```go
limiter := policy.NewTokenBucketLimiter(100, 10, 60, 1_000_000)

r := chi.NewRouter()
r.Use(middleware.RealIP)
r.Use(radixipchi.Middleware(radixipchi.Config{
    Engine:         engineAdapter,
    Limiter:        limiter,
    Blocklist:      true,
    RateLimit:      true,
    TrustedProxies: []string{"10.0.0.0/8"},
}))
```

For the shared YAML schema, route-specific limits, auto-ban, and security guidance, see the [middleware guide](../../../../docs/middleware.md).

## License

MIT
