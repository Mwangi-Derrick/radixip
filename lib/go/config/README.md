# RadixIP Go configuration

`config.LoadFromFile` and `config.ParseYAML` load and validate the shared YAML
schema used by the Go and Rust policy engines. TOML is not currently parsed by
these loaders; `config/radixip.toml` is a reference only.

The [runnable rate-limit example](../../../config/examples/rate-limit-algorithms.yaml)
shows the five algorithm names, global defaults, and route-specific overrides.
The [field guide](../../../docs/rate-limiting.md) explains `capacity`,
`refill_rate`, `window_seconds`, `bucket_mode`, and cache settings.
The [full configuration guide](../../../docs/configuration.md) documents the
other top-level sections.

```go
cfg, err := config.LoadFromFile("config/radixip.yaml")
if err != nil {
	log.Fatal(err)
}
fmt.Println(cfg.RadixIP.RateLimit.Algorithm)
```
