# radixip-config

Configuration types and hot-reload loader for [RadixIP](https://github.com/Mwangi-Derrick/radixip) middleware.

## Usage

Add to your `Cargo.toml`:

```toml
[dependencies]
radixip-config = "0.1"
```

`RadixIpConfig::from_file` reads the shared YAML format. The TOML file in
`config/radixip.toml` is a reference only; TOML loading is not implemented.
The [runnable algorithm example](../../../config/examples/rate-limit-algorithms.yaml)
shows global settings and route overrides. See the
[configuration field guide](../../../docs/rate-limiting.md) for `capacity`,
`refill_rate`, `window_seconds`, bucket modes, TTL, and algorithm behavior.
The [full configuration guide](../../../docs/configuration.md) explains the
other shared YAML sections.

## License

MIT
