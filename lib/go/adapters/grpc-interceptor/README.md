# RadixIP Go gRPC Interceptor

Hot-reloading unary and stream interceptors for [gRPC-Go](https://grpc.io/docs/languages/go/), with blocklist checks, token-bucket rate limiting, route policies, auto-ban, and retry metadata.

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

See the [middleware guide](../../../../docs/middleware.md) for the shared YAML schema and gRPC integration details.

## License

MIT
