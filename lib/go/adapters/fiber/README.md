# RadixIP Fiber Middleware

RadixIP middleware for [Fiber](https://gofiber.io/), with blocklist checks, token-bucket rate limiting, trusted-proxy IP extraction, route policies, auto-ban, and YAML hot reload.

## Install

```bash
go get github.com/Mwangi-Derrick/radixip/lib/go/adapters/fiber
```

## Hot-reloaded configuration

```go
mw, stop, err := radixipfiber.NewFromYAML("radixip.yaml", engineAdapter)
if err != nil {
    log.Fatal(err)
}
defer stop()

app.Use(mw)
```

`engineAdapter` implements `Lookup(string) bool`. The middleware evaluates the shared YAML policy before the Fiber handler and supports trusted proxies, global limits, route limits, and auto-ban configuration.

## Static configuration

```go
limiter := policy.NewTokenBucketLimiter(100, 10, 60, 1_000_000)
app.Use(radixipfiber.Middleware(radixipfiber.Config{
    Engine:        engineAdapter,
    Limiter:       limiter,
    Blocklist:     true,
    RateLimit:     true,
    TrustedProxies: []string{"10.0.0.0/8"},
}))
```

See the [middleware guide](../../../../docs/middleware.md) for the shared YAML schema and IP extraction rules.

## License

MIT
