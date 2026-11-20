# RadixIP Go gRPC Interceptor

Hot-reloading unary and stream interceptors for [gRPC-Go](https://grpc.io/docs/languages/go/), with blocklist checks, configurable rate limiting, route policies, auto-ban, and retry metadata.

## Install

```bash
go get github.com/Mwangi-Derrick/radixip/lib/go/adapters/grpc-interceptor
```

## Usage

```go
unary, stream, stop, err := radixipgrpc.NewFromYAML("radixip.yaml", engineAdapter)
if err != nil {
    log.Fatal(err)
}
defer stop()

server := grpc.NewServer(
    grpc.UnaryInterceptor(unary),
    grpc.StreamInterceptor(stream),
)
radixipv1.RegisterRadixServiceServer(server, service)
```

The engine adapter implements `Lookup(string) bool`. Clients may send `x-forwarded-for` or `x-real-ip` metadata. Rate-limited calls return `ResourceExhausted` with `retry-after: 1`; blocklisted and auto-banned calls return `PermissionDenied`.

`NewCombinedFromYAML` is available when one value should provide both interceptor methods. Use one interceptor path per server request; do not stack equivalent RadixIP interceptors because that would consume limiter tokens twice.

The [shared YAML example](../../../../config/examples/rate-limit-algorithms.yaml)
includes global and route policies for every limiter. See the
[rate-limit field guide](../../../../docs/rate-limiting.md) for algorithm
semantics and settings, including `capacity`, `refill_rate`, and
`window_seconds`.
For engine, proxy, blocklist, XDP, auto-ban, and metrics fields, see the
[full configuration guide](../../../../docs/configuration.md).

For gRPC request integration details, see the [middleware guide](../../../../docs/middleware.md).

## License

MIT
