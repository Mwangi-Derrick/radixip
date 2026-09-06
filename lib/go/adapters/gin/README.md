# RadixIP Gin Middleware

RadixIP middleware for [Gin](https://github.com/gin-gonic/gin), with blocklist checks, token-bucket rate limiting, trusted-proxy IP extraction, route policies, auto-ban, and YAML hot reload.

## Install

```bash
go get github.com/Mwangi-Derrick/radixip/lib/go/adapters/gin
```

## Hot-reloaded configuration

```go
mw, stop, err := radixipgin.NewFromYAML("radixip.yaml", engineAdapter)
if err != nil {
    log.Fatal(err)
}
defer stop()

router.Use(mw)
```

`engineAdapter` implements `Lookup(string) bool` for blocklist checks. The middleware reads `X-Forwarded-For`, `X-Real-IP`, or the peer address, applies the shared YAML policy, and returns the configured blocked or rate-limited status.

## Static configuration

```go
limiter := policy.NewTokenBucketLimiter(100, 10, 60, 1_000_000)
router.Use(radixipgin.Middleware(radixipgin.Config{
    Engine:         engineAdapter,
    Limiter:         limiter,
    Blocklist:       true,
    RateLimit:       true,
    TrustedProxies:  []string{"10.0.0.0/8"},
}))
```

For the shared YAML schema, route-specific limits, auto-ban, and security guidance, see the [middleware guide](../../../../docs/middleware.md).

## License

MIT
