# radixip-policy

Policy engine for [RadixIP](https://github.com/Mwangi-Derrick/radixip): IP blocklists, rate limiting, and auto-ban logic.

## Usage

```toml
[dependencies]
radixip-policy = "0.1"
```

`PolicyEngine` and `ConfigWatcher` select the limiter from the shared YAML
policy config. The [runnable example](../../../config/examples/rate-limit-algorithms.yaml)
includes all five algorithms, including per-route overrides. See the
[rate-limit field guide](../../../docs/rate-limiting.md) for how `capacity`,
`refill_rate`, `window_seconds`, and `bucket_mode` affect each algorithm.
The [full configuration guide](../../../docs/configuration.md) explains the
other shared YAML sections.

## License

MIT
