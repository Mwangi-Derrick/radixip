# radixip-rs

[![Crates.io](https://img.shields.io/crates/v/radixip-rs.svg)](https://crates.io/crates/radixip-rs)
[![docs.rs](https://docs.rs/radixip-rs/badge.svg)](https://docs.rs/radixip-rs)
[![Rust](https://img.shields.io/badge/Rust-1.97.1-orange?logo=rust)](https://www.rust-lang.org/)
[![Benchmarks](https://github.com/Mwangi-Derrick/radixip/actions/workflows/bench.yml/badge.svg)](https://github.com/Mwangi-Derrick/radixip/actions/workflows/bench.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**radixip-rs** is a high-performance, lock-free IP subnet matching engine with three configurable tree implementations: uncompressed binary trie, compressed Patricia/radix tree, and Adaptive Radix Tree (ART). It supports IPv4 and IPv6, zero-alloc lookups in ART, and optional Redis-based distributed synchronization.

The crate is the Rust core of the RadixIP ecosystem. It is also used by the C-ABI FFI layer, which exposes the engine to Python, Node.js, and C/C++ consumers.

---

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Tree Types](#tree-types)
  - [Uncompressed Tree](#1-uncompressed-tree-binary-trie)
  - [Compressed Tree](#2-compressed-tree-binary-patricia-tree)
  - [Adaptive Radix Tree](#3-adaptive-radix-tree-art)
- [Engine Variants](#engine-variants)
- [Node Variants](#node-variants)
- [Metadata](#metadata)
- [IPv6 Support](#ipv6-support)
- [Redis Synchronization](#redis-synchronization)
- [Performance](#performance)
- [C-ABI FFI](#c-abi-ffi)
- [Features](#features)
- [Benchmarking](#benchmarking)
- [Safety](#safety)
- [Roadmap](#roadmap)
- [License](#license)

---

## Installation

```toml
[dependencies]
radixip-rs = "0.1.0"
```

### Optional Features

```toml
[dependencies]
radixip-rs = {
    version = "0.1.0",
    features = ["redis"]  # Enable Redis Pub/Sub synchronization
}
```

---

## Quick Start

RadixIP performs **longest-prefix matching**: when multiple prefixes contain an address, the most specific matching prefix is selected. This makes the data structure suitable for routing tables, forwarding information bases (FIBs), policy tables, and other prefix-based lookups.

```rust
use ipnetwork::IpNetwork;
use radixip_rs::{new_memory_efficient, Metadata};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Create a memory-efficient engine (compressed Patricia tree)
    let engine = new_memory_efficient().await;

    // Insert a subnet with metadata
    let prefix: IpNetwork = "10.0.0.0/8".parse()?;
    let meta = Metadata::new("allow")
        .with_attribute("region", "us-east-1")
        .with_attribute("asn", "AS12345");
    engine.insert(prefix, meta)?;

    // Longest prefix match lookup
    let ip = "10.1.2.3".parse()?;
    if let Some(metadata) = engine.lookup(&ip) {
        println!("Match: {}", metadata.value);
        for (key, val) in metadata.attributes() {
            println!("  {}: {}", key, val);
        }
    }

    Ok(())
}
```

---

## Tree Types

RadixIP provides three tree implementations with different performance characteristics. Choose based on your workload.

**At a glance:** use the uncompressed trie when update/write performance and implementation simplicity matter most; use the compressed tree when memory efficiency and data-plane lookups matter; use ART when minimizing lookup latency and read-path allocations is the priority.

### 1. Uncompressed Tree (Binary Trie)

The `UncompressedTree` implements a standard binary trie where each level represents one bit of the IP address.

**Characteristics:**

- O(prefix\_length) writes
- O(prefix\_length) reads
- No path compression
- Simple implementation, easy to reason about
- Fastest writes

**Best for:** Control planes, BGP route collectors, environments with frequent updates

```rust
use radixip_rs::{new_control_plane, NodeVariant};

let engine = new_control_plane().await;
// Creates an EngineWrapper with UncompressedTree
```

### 2. Compressed Tree (Binary Patricia Tree)

The `CompressedTree` implements a Patricia trie with branch compression, reducing memory usage by eliminating single-child nodes.

**Characteristics:**

- O(k) reads where k is number of branching points
- 4x memory savings over uncompressed trie
- Path compression eliminates redundant nodes
- Optimized for data-plane workloads

**Best for:** Packet forwarding, FIB (Forwarding Information Base), routing tables

```rust
use radixip_rs::{new_data_plane, NodeVariant};

let engine = new_data_plane().await;
// Creates an EngineWrapper with CompressedTree
```

### 3. Adaptive Radix Tree (ART)

The `AdaptiveRadixTree` dynamically selects the optimal node size (Node4, Node16, Node48, Node256) based on the number of children. Node16 uses SIMD acceleration.

**Characteristics:**

- Dynamic node sizes (4, 16, 48, 256 children)
- **Zero allocations on the read path**
- SIMD-accelerated Node16 (SSE4.1/AVX2 on x86, NEON on ARM)
- Auto-upgrade/downgrade between node types
- Cache-line aligned structures

**Best for:** Ultra-high-performance routing, API gateways, DDoS mitigation

```rust
use radixip_rs::{new_art, NodeVariant};

let engine = new_art().await;
// Creates an EngineWrapper with AdaptiveRadixTree
```

### Comparison Table

| **FeatureUncompressedCompressed (Patricia)ART** |               |             |                          |
| ----------------------------------------------- | ------------- | ----------- | ------------------------ |
| Write throughput                                | ⚡⚡ Fastest    | 🟡 Moderate | 🟡 Moderate              |
| Read throughput                                 | 🟡 Good       | ⚡ Fast      | ⚡⚡ Fastest               |
| Memory (500K routes)                            | \~4–8 MB      | \~1–2 MB    | \~1.5–3 MB               |
| Allocations on read                             | Yes           | Yes         | **No**                   |
| SIMD acceleration                               | No            | No          | **Yes** (Node16)         |
| Path compression                                | No            | **Yes**     | **Yes**                  |
| Dynamic node sizes                              | No            | No          | **Yes**                  |
| Best for                                        | Control plane | FIB         | High-performance routing |

---

## Engine Variants

RadixIP provides three engine wrappers that determine concurrency behavior.

### StandardEngine

Single-threaded engine with no locking overhead. Ideal for single-threaded workloads or when you need to manage synchronization externally.

```rust
use radixip_rs::{StandardEngine, CompressedTree, NodeVariant};

let tree = CompressedTree::new(NodeVariant::NormalRadixNode);
let engine = StandardEngine::new(tree);
```

### ConcurrentEngine

Lock-free engine with atomic operations. Supports concurrent lookups from multiple threads without contention. Inserts are sequential but use atomic pointers.

```rust
use radixip_rs::{ConcurrentEngine, CompressedTree, NodeVariant};

let tree = CompressedTree::new(NodeVariant::AtomicRadixNode);
let engine = ConcurrentEngine::new(tree);
```

**Concurrency model:**

- Reads: Fully concurrent, no locks (uses Arc, AtomicPtr)
- Writes: Sequential but thread-safe (uses atomic CAS)
- No reader-writer contention

### ART Engine

Specialized engine for Adaptive Radix Tree. Provides the same concurrency guarantees as `ConcurrentEngine` but with ART-specific optimizations.

```rust
use radixip_rs::{ARTEngine, NodeVariant};

let engine = ARTEngine::new(NodeVariant::NormalRadixNode);
```

### Convenience Functions

```rust
// Memory-efficient: CompressedTree with NormalRadixNode
let engine = radixip_rs::new_memory_efficient().await;

// Control plane: UncompressedTree with NormalTrieNode
let engine = radixip_rs::new_control_plane().await;

// Data plane: CompressedTree with NormalRadixNode
let engine = radixip_rs::new_data_plane().await;

// Ultra-fast: ART with NormalRadixNode
let engine = radixip_rs::new_art().await;
```

---

## Node Variants

Node variants determine the memory layout and synchronization strategy of each tree node.

### NormalTrieNode / NormalRadixNode

Standard nodes with no atomic operations. Fastest for single-threaded workloads.

```rust
NodeVariant::NormalTrieNode    // Uncompressed
NodeVariant::NormalRadixNode   // Compressed
```

### AtomicTrieNode / AtomicRadixNode

Nodes with atomic reference counting for safe concurrent lookups.

```rust
NodeVariant::AtomicTrieNode    // Uncompressed
NodeVariant::AtomicRadixNode   // Compressed
```

### PaddedTrieNode / PaddedRadixNode

Cache-line aligned nodes to prevent false sharing in multi-threaded environments.

```rust
NodeVariant::PaddedTrieNode    // Uncompressed
NodeVariant::PaddedRadixNode   // Compressed
```

### LockFreeTrieNode / LockFreeRadixNode

Nodes using CAS (Compare-And-Swap) operations for lock-free inserts.

```rust
NodeVariant::LockFreeTrieNode    // Uncompressed
NodeVariant::LockFreeRadixNode   // Compressed
```

### Selection Guide

| **Node VariantUse Case** |                                            |
| ------------------------ | ------------------------------------------ |
| `Normal*`                | Single-threaded, maximum performance       |
| `Atomic*`                | Concurrent reads, infrequent writes        |
| `Padded*`                | Multi-threaded with false sharing concerns |
| `LockFree*`              | Concurrent reads and writes                |

---

## Metadata

The `Metadata` struct stores arbitrary data associated with a subnet.

```rust
use radixip_rs::Metadata;

// Create metadata
let meta = Metadata::new("allow")
    .with_attribute("region", "us-east-1")
    .with_attribute("asn", "AS12345")
    .with_attribute("ttl", "3600");

// Access data
println!("Value: {}", meta.value);
for (key, val) in meta.attributes() {
    println!("{}: {}", key, val);
}
```

**Attributes:** Store any key-value string data. Common uses:

- Geographic region (`region: us-east-1`)
- Autonomous System Number (`asn: AS12345`)
- Security tier (`tier: gold`)
- TTL or expiration (`ttl: 3600`)
- Custom application-specific metadata

---

## IPv6 Support

RadixIP fully supports IPv6 with identical API and performance characteristics.

```rust
use ipnetwork::IpNetwork;

// IPv6 prefix
let prefix: IpNetwork = "2001:db8::/32".parse()?;
engine.insert(prefix, Metadata::new("v6-network"))?;

// IPv6 lookup
let ip = "2001:db8:1234:5678::1".parse()?;
if let Some(meta) = engine.lookup(&ip) {
    println!("IPv6 match: {}", meta.value);
}
```

**Performance note:** IPv6 lookups traverse 128 bits vs 32 bits for IPv4, but path compression in compressed trees and ART reduces this to a manageable constant factor.

---

## Redis Synchronization

Redis is used for distributed cache synchronization, not for lookups. All lookups are performed entirely in local memory.

```rust
use radixip_rs::{RadixConfig, new};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let mut config = RadixConfig::new();
    config.enable_split_plane = true;
    config.redis_url = Some("redis://localhost:6379".to_string());
    config.redis_channel = "radixip:updates".to_string();

    let engine = new(config).await?;

    // Insertions are automatically published to Redis
    let prefix: IpNetwork = "192.168.0.0/16".parse()?;
    engine.insert(prefix, Metadata::new("internal"))?;

    // All nodes in the cluster receive the update via Pub/Sub
    // and insert it into their local tree

    Ok(())
}
```

**Architecture:**

- Redis **not** on the critical path for lookups
- Lookups: Local memory only (60-700 ns)
- Updates: Propagate via Redis Pub/Sub (<1 ms)
- Cache hydration: Redis persistence backing store

---

## Performance

### CI Benchmark Results

Representative results from GitHub Actions runner (Intel Xeon Platinum 8573C):

| **Structure** | **Insert / prefix** | **Concurrent lookup** | **Allocations** |
| --- | ---: | ---: | ---: |
| **ART**                                                  | 152.2 ns | **60.9 ns** | 0 B/op      |
| Patricia/RadixNode                                       | 2,351 ns | 177.7 ns    | Not tracked |
| Binary Trie                                              | 5,055 ns | 728.6 ns    | Not tracked |

**Throughput:**

- ART: **16.4 million** lookups/second
- Patricia: **5.6 million** lookups/second
- Binary Trie: **1.4 million** lookups/second

### Sequential Lookup (Go Comparison)

While Rust is the primary focus, the Go ART implementation achieves similar performance:

| **StructureHit lookupMiss lookupAllocations** |          |         |                  |
| --------------------------------------------- | -------- | ------- | ---------------- |
| Go ART                                        | 27.0 ns  | 20.8 ns | 0 B/op, 0 allocs |
| Go Patricia                                   | 167.0 ns | 61.5 ns | 24 B/op, 1 alloc |

### Running Your Own Benchmarks

```bash
# All benchmarks
cargo bench

# Specific groups
cargo bench --bench lookup_bench -- insert
cargo bench --bench lookup_bench -- concurrent_lookup
cargo bench --bench lookup_bench -- lookup

# Compare to baseline
cargo bench -- --baseline main
```

See the [Benchmark Methodology Guide](https://../docs/guides/benchmark-methodology.md) for detailed explanation of measurement techniques.

---

## C-ABI FFI

The library exports a C-compatible API for integration with other languages.

### C/C++

```c
#include "radixip_rs.h"

// Create engine
RadixEngine* engine = radix_engine_new();

// Insert CIDR with JSON metadata
radix_engine_insert(engine, "192.168.0.0/16", "{\"region\": \"local\"}");

// Lookup
bool found = radix_engine_match(engine, "192.168.1.100");
// found == true

// Get metadata (returns JSON string)
const char* meta = radix_engine_lookup_metadata(engine, "192.168.1.100");

// Cleanup
radix_engine_free(engine);
```

### Python (PyO3)

```python
from radixip import Engine

engine = Engine()
engine.insert("192.168.0.0/16", {"region": "local"})
result = engine.lookup("192.168.1.100")
# result == {"region": "local"}
```

### Node.js (N-API)

```javascript
const { Engine } = require('radixip');

const engine = new Engine();
engine.insert("192.168.0.0/16", { region: "local" });
const result = engine.lookup("192.168.1.100");
// result == { region: "local" }
```

---

## Features

### `redis` (optional)

Enables Redis Pub/Sub synchronization for distributed deployments.

```toml
radixip-rs = { version = "0.1.0", features = ["redis"] }
```

**Dependencies added:**

- `redis` crate for client
- `tokio` runtime for async

**Configuration:**

```rust
let config = RadixConfig {
    enable_split_plane: true,
    redis_url: Some("redis://localhost:6379".to_string()),
    redis_channel: "radixip:updates".to_string(),
    cache_ttl_seconds: 3600,
    max_cache_size: 1_000_000,
    ..Default::default()
};
```

---

## Safety

RadixIP prioritizes safety without sacrificing performance:

- **No ****`unsafe`**** code** in the core tree implementations
- **Atomic operations** for lock-free reads (`Arc`, `AtomicPtr`)
- **Send + Sync** for concurrent use across threads
- **Zero-cost abstractions** — generics compile to optimal machine code
- **Comprehensive test suite** with property-based testing

**Memory safety guarantees:**

- No use-after-free
- No data races
- No iterator invalidation
- No double-free

---

## License

MIT License - See [LICENSE](https://github.com/Mwangi-Derrick/radixip/blob/main/LICENSE) for details.

---

Built with ❤️ by [Derrick Mwangi](https://github.com/Mwangi-Derrick) and the team at [Resplix](https://resplix.com/).

**High-performance IP subnet matching for modern infrastructure.**

⭐ Star us on [GitHub](https://github.com/Mwangi-Derrick/radixip) if you find this crate useful!
