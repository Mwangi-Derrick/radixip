# RadixIP Rust Core

This is the blazing fast (up to 83M ops/sec), zero-allocation-on-read engine at the heart of RadixIP. It powers the C-ABI FFI layer, and by extension, the Python, Node.js, and C++ bindings.

## Installation

Add this to your `Cargo.toml`:

```toml
[dependencies]
radixip-rs = { version = "0.1.0", features = ["redis"] }
```

## Quick Start

```rust
use ipnetwork::IpNetwork;
use radixip_rs::{new_memory_efficient, Metadata};

#[tokio::main]
async fn main() {
    // new_memory_efficient() gives you the StandardEngine backed by a Compressed Patricia Tree.
    // It is optimal for environments that don't need heavy concurrent sharding.
    let engine = new_memory_efficient().await;
    
    // Insert
    let prefix: IpNetwork = "10.0.0.0/8".parse().unwrap();
    let meta = Metadata::new("allow").with_attribute("asn", "AS12345");
    engine.insert(prefix, meta).unwrap();
    
    // Lookup
    let ip = "10.1.2.3".parse().unwrap();
    if let Some(meta) = engine.lookup(&ip) {
        println!("Match: {}", meta.value); // "allow"
    }
}
```

## Hybrid Engine (Split-Plane)

If you are running a highly concurrent API gateway where routes are constantly updated by background workers, use the Hybrid architecture:

```rust
use radixip_rs::{new, RadixConfig, EngineVariant};

#[tokio::main]
async fn main() {
    let mut config = RadixConfig::new();
    config.enable_split_plane = true;
    config.engine_variant = EngineVariant::Concurrent; // Sharded control plane
    
    // This will spawn background Tokio tasks to handle Redis synchronization automatically
    let engine = new(config).await;
}
```

## Benchmarking

Run the criterion benchmarking suite to see the performance on your hardware:

```bash
cd lib/rust
cargo bench
```
