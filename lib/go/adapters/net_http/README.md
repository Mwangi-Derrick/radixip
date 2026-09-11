# RadixIP net/http Middleware

RadixIP middleware for [net/http](https://pkg.go.dev/net/http), with blocklist checks, token-bucket rate limiting, trusted-proxy IP extraction, route policies, auto-ban, and YAML hot reload.

Because the middleware uses the standard `func(http.Handler) http.Handler` signature, it works unchanged with any router built on `net/http` — chi, gorilla/mux, httprouter, and the standard library `ServeMux`.

## Install

```bash
go get github.com/Mwangi-Derrick/radixip/lib/go/adapters/nethttp
```

## Hot-reloaded configuration

```go
mw, stop, err := radixipnethttp.MiddlewareFromYAML("radixip.yaml", engineAdapter)
if err != nil {
    log.Fatal(err)
}
defer stop()

handler := mw(yourHandler)
http.Handle("/", handler)
```

`engineAdapter` implements `Lookup(string) bool` for blocklist checks. The middleware reads `X-Forwarded-For`, `X-Real-IP`, or the peer address, applies the shared YAML policy, and returns the configured blocked or rate-limited status.

If you want a terminal handler mounted at a specific route instead of a middleware layer, use `NewFromYAML`:

```go
h, stop, err := radixipnethttp.NewFromYAML("radixip.yaml", engineAdapter)
if err != nil {
    log.Fatal(err)
}
defer stop()

http.Handle("/api/", h)
```

## Static configuration

```go
limiter := policy.NewTokenBucketLimiter(100, 10, 60, 1_000_000)

mw := radixipnethttp.Middleware(radixipnethttp.Config{
    Engine:         engineAdapter,
    Limiter:        limiter,
    Blocklist:      true,
    RateLimit:      true,
    TrustedProxies: []string{"10.0.0.0/8"},
})

http.Handle("/", mw(yourHandler))
```

For the shared YAML schema, route-specific limits, auto-ban, and security guidance, see the [middleware guide](../../../../docs/middleware.md).

## License

MIT
