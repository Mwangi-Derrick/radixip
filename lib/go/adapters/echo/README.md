# RadixIP Echo Middleware

RadixIP middleware for [Echo](https://echo.labstack.com/), with blocklist checks, configurable rate limiting, trusted-proxy IP extraction, route policies, auto-ban, and YAML hot reload.

## Install

```bash
go get github.com/Mwangi-Derrick/radixip/lib/go/adapters/echo
```

## Hot-reloaded configuration

```go
mw, stop, err := radixipecho.NewFromYAML("radixip.yaml", engineAdapter)
if err != nil {
    log.Fatal(err)
}
defer stop()

e.Use(mw)
```

`engineAdapter` implements `Lookup(string) bool`. The middleware evaluates the shared YAML policy before the Echo handler and returns configured responses for invalid, blocked, or rate-limited requests.

## Rate-limit YAML

`NewFromYAML` accepts the shared policy schema. The
[algorithm example](../../../../config/examples/rate-limit-algorithms.yaml)
shows global defaults and route overrides; the [field guide](../../../../docs/rate-limiting.md)
explains every limiter and setting. `algorithm`, `capacity`, `refill_rate`, and
`window_seconds` determine the selected policy's behavior.
For engine, proxy, blocklist, XDP, auto-ban, and metrics fields, see the
[full configuration guide](../../../../docs/configuration.md).

## Static configuration

```go
limiter := policy.NewTokenBucketLimiter(100, 10, 60, 1_000_000)
e.Use(radixipecho.Middleware(radixipecho.Config{
    Engine:        engineAdapter,
    Limiter:       limiter,
    Blocklist:     true,
    RateLimit:     true,
    TrustedProxies: []string{"10.0.0.0/8"},
}))
```

See the [middleware guide](../../../../docs/middleware.md) for the shared YAML schema and deployment guidance.

## License

MIT
