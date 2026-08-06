# RadixIP

[![Go Reference](https://img.shields.io/badge/Go-1.26.1-00ADD8?logo=go)](https://github.com/Mwangi-Derrick/radixip)
[![Rust](https://img.shields.io/badge/Rust-1.97.1-orange?logo=rust)](https://github.com/Mwangi-Derrick/radixip)
[![CI](https://github.com/Mwangi-Derrick/radixip/actions/workflows/bench.yml/badge.svg)](https://github.com/Mwangi-Derrick/radixip/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/Mwangi-Derrick/radixip)](https://goreportcard.com/report/github.com/Mwangi-Derrick/radixip)

> **The missing link between network security, DDoS mitigation, and geolocation caching.**
>
> RadixIP gives you IP filtering at **45ns per lookup**—self-hosted and open source.
> Block attacks, secure databases, and save **$3.6M/year** on geolocation APIs.

> 🚀 **Go:** ~45ns/lookup · 🦀 **Rust:** ~12ns/lookup · 🔌 **FFI:** Native performance from any language

## 🎯 What is RadixIP?

RadixIP is a production-grade IP subnet caching engine that solves a critical infrastructure problem:

**The Problem**: Standard hash maps can't efficiently match IPs against dynamic CIDR blocks (`/8`, `/16`, `/24`, `/32`) at scale. Database ACLs, API gateways, and edge proxies need sub-microsecond lookups with zero GC pressure.

**The Solution**: A lock-free binary radix tree with L1 (in-memory) + L2 (Redis look-aside) architecture, enabling:
- **~45ns** LPM lookups in Go
- **~12ns** LPM lookups in Rust
- **Zero heap allocations** on the read path
- **Instant global sync** via Redis Pub/Sub
- **Multi-language support** through C-FFI bindings

**The Impact**: Drop malicious traffic, enforce dynamic whitelisting, and route connections at memory speeds.

## 🎯 Quick Start Example

### Block a DDoS attack in 3 lines of code

```go
// 1. Detect attack from 192.168.1.0/24
radixEngine.Insert("192.168.1.0/24", "malicious")

// 2. All future requests from that subnet are blocked instantly
_, found := radixEngine.Match(netip.MustParseAddr("192.168.1.100"))
// found = true → BLOCKED

// 3. Propagate to all nodes via Redis
redis.Publish("security:blocklist", "192.168.1.0/24")
```

# RadixIP Documentation

This is where RadixIP's design decisions are explained in depth — not just *what*
the library does, but *why* it's built this way. The main repo README stays
short on purpose; this folder is where the engineering reasoning lives.

## Core Concepts

| Document | notes |
|---|---|
| [Architecture](./docs/guides/architecture.md) | How the L1 (in-process) / L2 (Redis) layers fit together |
[Sharding Architecture](./docs/guides/sharding-architecture.md) | Scaling with sharding |
| [Radix Tree Design](./docs/guides/radix-tree-design.md) | The data structure at the core of RadixIP, and why it beats a hashmap or standard trie for this problem |
| [Longest Prefix Match](./docs/guides/longest-prefix-match.md) | The algorithm every IP router on the internet runs, explained from first principles |
| [IPv4 vs IPv6](./docs/guides/ipv4-vs-ipv6.md) | How address structure differs, and what that means for caching strategy |
| [Cache Locality](./docs/guides/cache-locality.md) | Why memory access patterns usually matter more than algorithmic complexity |
| [How Routers Work](./docs/guides/how-routers-work.md) | The real-world context RadixIP borrows from |
| [Benchmark Methodology](./docs/guides/benchmark-methodology.md) | Exactly how the numbers in the README were produced, so you can reproduce or challenge them |

## Reading order

If you're new to networking data structures, read in this order:

1. [How Routers Work](./docs/guides/how-routers-work.md) — the motivating context
2. [Longest Prefix Match](./docs/guides/longest-prefix-match.md) — the algorithm
3. [Radix Tree Design](./docs/guides/radix-tree-design.md) — the data structure that makes it fast
4. [IPv4 vs IPv6](./docs/guides/ipv4-vs-ipv6.md) — how it changes across protocols
5. [Cache Locality](./docs/guides/cache-locality.md) — why the implementation is shaped the way it is
6. [Architecture](./docs/guides/architecture.md) — how it's wired into a real system
7. [Benchmark Methodology](./docs/guides/benchmark-methodology.md) — how to verify all of the above


## 🧠 Design Goals

RadixIP is designed around a few core principles:

- ⚡ Zero allocations on the read path
- 🔒 Lock-free lookups for highly concurrent workloads
- 🌳 Efficient longest-prefix matching (LPM)
- 💾 Cache-conscious memory layout
- 🌐 Cross-language interoperability through C ABI
- 🔄 Distributed cache synchronization via Redis Pub/Sub
- 📈 Predictable performance under heavy read workloads

---

# 🌲 Why Radix Trees?

IP subnet matching is fundamentally different from exact-key lookups.

Given an address like:

```text
192.168.1.42
```

the engine must determine the **longest matching prefix**:

```text
192.168.0.0/16
192.168.1.0/24
192.168.1.32/27
```

A traditional hash map can efficiently answer:

> "Does this exact key exist?"

It cannot efficiently answer:

> "Which CIDR prefix best matches this address?"

This makes radix trees a natural fit for routing tables, ACLs, reverse proxies, firewalls, and API gateways.

---

## 📊 Data Structure Comparison

| Structure | Exact Match | Longest Prefix Match | Memory | Notes |
|------------|------------|----------------------|--------|------|
| Hash Map | ✅ Excellent | ❌ No | Medium | Best for exact keys |
| Standard Trie | ✅ | ✅ | High | Large number of nodes |
| Patricia / Radix Tree | ✅ | ✅ | Low | Path compression reduces memory |
| Binary Radix Tree | ✅ | ✅ | Low | Well suited for IPv4 bit traversal |

---

## 💡 Why Radix Trees Work Well

Every IPv4 address is only **32 bits**.

Instead of hashing an address, RadixIP walks those bits directly.

```text
IP Address

11000000 10101000 00000001 01101010
│
├── 1
│   ├── 1
│   │   ├── 0
│   │   └── ...
```

Traversal is deterministic and naturally supports longest-prefix matching.

No additional indexing structure is required.

---

## 💾 Memory Locality

Modern processors spend far more time waiting for memory than executing instructions.

Approximate memory hierarchy:

| Memory | Typical Latency |
|---------|----------------:|
| CPU Register | <1 ns |
| L1 Cache | ~1 ns |
| L2 Cache | ~4 ns |
| L3 Cache | ~10–20 ns |
| Main Memory | ~60–100 ns |

Reducing cache misses often has a larger impact than reducing arithmetic operations.

For that reason, RadixIP focuses on:

- compact node layouts
- minimizing unnecessary indirection
- reducing allocations
- improving spatial locality
- keeping frequently traversed nodes hot in cache where possible

---

## 🔬 Cache-Conscious Design

Rather than optimizing only algorithmic complexity, RadixIP also considers modern CPU behavior.

Examples include:

- branch-compressed Patricia nodes
- compact metadata storage
- zero-allocation read path
- predictable traversal
- cache-line friendly structures where practical

These optimizations reduce memory pressure and improve throughput on large routing tables.

---

## 🔄 Why Redis?

Redis is **not** part of the lookup path.

Every lookup is performed entirely inside the local in-memory radix tree.

Redis is used only for:

- distributing subnet updates
- cache invalidation
- Pub/Sub synchronization
- persistence coordination

The request path remains:

```text

Incoming Request
        │
        ▼
 Local Radix Tree
        │
        ▼
 Decision
```

Redis is only consulted when routing information changes.

---

## ⚙️ Complexity

| Operation | Complexity |
|-----------|------------|
| Insert | O(prefix length) |
| Delete | O(prefix length) |
| Lookup | O(address bits) |
| Memory | O(number of prefixes) |

Since IPv4 addresses are only 32 bits, lookup time is effectively bounded by a small constant.

---

## 📈 Benchmark Methodology

Benchmarks are executed using:

- 10,000 CIDR prefixes
- 100,000 randomized lookups
- 80% hit rate / 20% miss rate
- warm caches
- release builds
- dedicated benchmark workflow in GitHub Actions

Results should be interpreted as measurements for the tested hardware and compiler versions rather than universal performance guarantees.

---

## 📊 Example Results

| Implementation | Lookup | Allocations |
|----------------|--------|-------------|
| Go | ~45 ns | 0 B/op |
| Rust | ~12 ns | 0 heap allocations |

See the CI artifacts for complete benchmark logs and hardware information.

---

## 🎯 Why It Matters

Algorithmic complexity is only part of the performance story.

On modern processors, memory access patterns frequently dominate execution time.

A well-designed data structure not only performs fewer operations—it performs them in a way that works *with* the processor's cache hierarchy rather than against it.

For networking, routing, and access-control workloads, that difference is often more important than asymptotic complexity alone.

## 🏗️ Architecture

### 🔄 The Hybrid L1/L2 Pipeline

Redis is completely removed from the critical lookup path. Every validation runs locally inside the application's process memory.

```text
                                    ┌──────────────────────────────────────────────┐
                                    │           API Gateway / Proxy Layer          │
                                    │    HTTP • gRPC • REST • Load Balancer        │
                                    └──────────────────┬───────────────────────────┘
                                                       │
                                                       ▼
┌────────────────────────────────────────────────────────────────────────────────────────────────────┐
│                               L1: RadixIP Engine                                                   │
│                                                                                                    │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────────────────────────────┐                    │
│  │ Go Module    │   │ Rust Crate   │   │ C ABI / FFI Bindings                 │                    │
│  │ radixip-go   │   │ radixip-rs   │   │ Python • Node.js • C/C++ • Zig       │                    │
│  └──────────────┘   └──────────────┘   └──────────────────────────────────────┘                    │
│                                                                                                    │
│  ┌──────────────────────────────────────────────────────────────────────────────────────────────┐  │
│  │ Lock-Free Binary Radix Tree                                                                  │  │
│  │ • Zero allocations on read                                                                   │  │
│  │ • Branch-compressed Patricia trie                                                            │  │
│  │ • Atomic pointer traversal                                                                   │  │
│  │ • Read-Copy-Update (RCU) friendly                                                            │  │
│  │ • Cache-aware node layout                                                                    │  │
│  └──────────────────────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                                    │
│  CPU Cache Optimizations                                                                          │
│  ┌──────────────────────────────────────────────────────────────────────────────────────────────┐  │
│  │ L1 Cache │ Hot traversal nodes │ Prefetched prefixes │ Pointer locality                     │  │
│  │ L2 Cache │ Frequently accessed subtrees │ Branch predictor friendly                         │  │
│  │ L3 Cache │ Shared radix segments across worker threads                                       │  │
│  │                                                                                              │  │
│  │ • Cache-line aligned structures (64-byte alignment)                                          │  │
│  │ • False-sharing avoidance                                                                     │  │
│  │ • NUMA-aware memory allocation                                                                │  │
│  │ • Software prefetching where beneficial                                                       │  │
│  └──────────────────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────┬──────────────────────────────────────────────────────┘
                                              │
                                              ▼
┌────────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                 L2: Redis Cache                                                   │
│                                                                                                    │
│  ┌──────────────────────────────────────────────────────────────────────────────────────────────┐  │
│  │ • Subnet → Metadata mappings                                                                  │  │
│  │ • Distributed cache                                                                           │  │
│  │ • Pub/Sub instant invalidation                                                                │  │
│  │ • Persistent backing store                                                                    │  │
│  │ • High availability                                                                           │  │
│  └──────────────────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────┬──────────────────────────────────────────────────────┘
                                              │
                                              ▼
┌────────────────────────────────────────────────────────────────────────────────────────────────────┐
│                               Persistent Storage                                                  │
│                                                                                                    │
│ • PostgreSQL                                                                                      │
│ • MySQL                                                                                           │
│ • SQLite                                                                                          │
│ • Custom IP metadata providers                                                                    │
└────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### Memory Layout

```text
                    CPU
                     │
          ┌──────────┼──────────┐
          │          │          │
         L1         L2         L3
      (32KB)     (512KB)    (Shared)
          │          │          │
          └──────────┼──────────┘
                     │
      Cache-Line Optimized Radix Nodes
                     │
      ┌──────────────────────────────┐
      │ Prefix                       │
      │ Left Pointer                 │
      │ Right Pointer                │
      │ Metadata Pointer             │
      │ Flags                        │
      │ Padding (64-byte aligned)    │
      └──────────────────────────────┘
                     │
             Main Memory (DRAM)
```

### Performance Strategy

| Layer | Purpose | Latency Target |
|--------|---------|----------------|
| L1 CPU Cache | Hot radix nodes | ~1 ns |
| L2 CPU Cache | Frequently traversed branches | ~4 ns |
| L3 CPU Cache | Shared worker data | ~12 ns |
| DRAM | Cold nodes | 60–100 ns |
| Redis | Distributed metadata cache | <1 ms |
| Database | Persistent storage | 5–20 ms |


## 🛠️ Production Use Cases

### 1. Database Security & Dynamic ACLs 🛡️
Protect databases (PostgreSQL, MySQL, MongoDB) by validating client IPs against dynamic whitelists at the proxy layer—before expensive authentication handshakes.

**Why RadixIP?** Standard firewall rule updates take seconds; RadixIP propagates new ACLs in milliseconds via Redis Pub/Sub.

### 2. High-Throughput API Gateways 🌐
Enforce enterprise security boundaries by filtering inbound requests against thousands of partner subnets (`/16`, `/24`) with zero perceivable overhead.

**The Edge**: Lock-free reads mean no mutex contention, even at 100k+ RPS.

### 3. Distributed DDoS Mitigation 🔥
When an attack is detected from an offending IP block, inject the subnet into Redis. All running instances pull the block into their local Radix tree instantly, dropping malicious traffic at the edge.

**The Result**: Block entire attack vectors in milliseconds, not minutes.

### 4. Multi-Tenant Infrastructure Platforms ☁️
Allow tenants to define custom IP whitelists for their isolated environments. RadixIP handles per-tenant subnet matching at memory speeds.

**The Scale**: Handle thousands of tenant-specific ACLs simultaneously.

### 5. Real Geolocation API Cost Killer 💰 

Geolocation APIs are expensive.

Commercial cloud geolocation API billing models scale linearly with lookups, quickly growing to massive monthly operational expenses at scale. RadixIP operates an intelligent hierarchical edge cache structure:

At scale, they can cost **$100,000+/month**.

RadixIP solves this with **intelligent IP caching**:

```text
[Incoming Request] ──> L1: Local Radix (45ns, FREE) ──[Hit]──> Return Metadata
                             │
                          [Miss]
                             ▼
                     L2: Distributed Redis (1ms, FREE) ──[Hit]──> Hydrate L1 & Return
                             │
                          [Miss]
                             ▼
                     L3: External Geo API ($$$) ──> Commit to Redis & Hydrate Tree
```

### The Cache Hierarchy

1. **L1: RadixIP** (45ns, FREE)
   - Caches exact IPs
   - Caches /24, /16 subnets
   - Caches ASN and country blocks

2. **L2: Redis** (1-5ms, FREE)
   - Shared across nodes
   - TTL 24-72 hours

3. **L3: Geo API** (10-100ms, $$$)
   - Only on cache miss
   - 90%+ request reduction

## Caching Strategy & Cost Savings

By caching both individual IPs and entire structural subnet masks locally, RadixIP can eliminate up to **90%+** of external network lookups.

### Inbound Metrics

| Traffic Volume | Traditional Costs | With RadixIP Cache Strategy | Monthly Opex Savings |
|----------------|-------------------|----------------------------|---------------------|
| 1,000,000 reqs / day | $100 / day | $10 / day | **90% Savings** |
| 10,000,000 reqs / day | $1,000 / day | $100 / day | **90% Savings** |
| 100,000,000 reqs / day | $10,000 / day | $1,000 / day | Annualized: ~$3.2M saved |

---

### 📊 Annualized Savings: **~$3.2M**

> **Key Insight:** RadixIP's intelligent subnet-aware caching reduces external lookup costs by 90% across all traffic volumes, delivering predictable and scalable cost savings.

### Why It Works

- **80%+ of IPs are repeat visitors** → cache hit
- **Subnet caching** → entire blocks cached at once
- **Zero-latency lookups** → no speed tradeoff
- **Automatic TTL** → fresh data when needed

## 📊 Performance Benchmarks

Benchmarks run against a high-entropy dataset of **10,000 subnets** and **100,000 interleaved IPs** (80% hits, 20% misses). Verified automatically on every commit via GitHub Actions.

| Implementation | Latency / Lookup | Throughput | Allocations | Memory |
|----------------|------------------|------------|-------------|--------|
| **Go (`radixip-go`)** | ~45 ns | ~22.2M ops/sec | 0 B/op | ~2 KB/node |
| **Rust (`radixip-rs`)** | ~12 ns | ~83.3M ops/sec | 0 heap alloc | ~16 bytes/node |
| **Rust FFI Layer** | ~18 ns | ~55.5M ops/sec | Fixed C-boundary | Minimal |
| **Reference (hashmap)** | ~150 ns | ~6.6M ops/sec | Significant | High |

**Why Rust is faster**:
- Zero-cost abstractions with no GC overhead
- Vectorized bit operations
- Better cache locality

**Why Go is still great**:
- Production-ready with excellent tooling
- Simpler concurrency model
- Easier integration with existing Go codebases

📥 **Audit the raw benchmark logs**: [Download from CI artifacts](https://github.com/Mwangi-Derrick/radixip/actions)

## 🚀 Getting Started

### Clone the repository
```bash
git clone https://github.com/Mwangi-Derrick/radixip.git
cd radixip

```

### Generate the dataset
```bash
python scripts/generate_mock_data.py --subnets 10000 --lookups 100000
```

Run Go benchmarks
``` bash
cd radixip-go
go test -bench=. -benchmem ./...
```
```bash
Run Rust benchmarks
bash
cd radixip-rs
cargo bench
```
### Use the Go library
```go
import "github.com/Mwangi-Derrick/radixip/radixip-go"

func main() {
    engine := radixip.NewEngine()
    engine.Insert("192.168.0.0/16", map[string]string{
        "region": "Nairobi",
        "isp": "Safaricom",
    })
    
    result, found := engine.Match(netip.MustParseAddr("192.168.1.100"))
    // found = true, result = metadata
}
```

### Use the Rust crate
```rust
use radixip_rs::Engine;

fn main() {
    let mut engine = Engine::new();
    engine.insert("192.168.0.0/16", metadata);
    
    if let Some(result) = engine.match_ip("192.168.1.100") {
        // result contains the metadata
    }
}
```

### Use the FFI layer (C-compatible)
```c
#include "radixip_rs.h"

RadixEngine* engine = radix_engine_new();
radix_engine_insert(engine, "192.168.0.0/16", metadata);
bool matched = radix_engine_match(engine, "192.168.1.100");
radix_engine_free(engine);
```

### ⚡ Quick Install

### Go
```bash
go get github.com/Mwangi-Derrick/radixip/radixip-go
```
### Rust
```rust
// add to Cargo.toml
[dependencies]
radixip-rs = "0.1.0"
```

### C/C++ (FFI)

## Download pre-built shared library
```curl
curl -LO https://github.com/Mwangi-Derrick/radixip/releases/latest/libradixip.so
```

## **CI Pipeline**

### 🔄 CI/CD Pipeline

Every commit to `main` triggers our continuous benchmarking pipeline:

1. **Generate** a high-entropy dataset (10k subnets, 100k IPs)
2. **Run** Go benchmarks (`go test -bench`)
3. **Run** Rust benchmarks (`cargo bench`)  
4. **Upload** raw performance logs as artifacts
5. **Fail** if performance regresses beyond 5%

**View results**: [GitHub Actions tab](https://github.com/Mwangi-Derrick/radixip/actions)

**Download artifacts**: Raw benchmark logs available for auditing.

## 🗺️ Roadmap

- [x] Go implementation with lock-free reads
- [x] Rust port with zero-cost abstractions  
- [x] C-FFI bindings for multi-language support
- [x] CI benchmarking pipeline
- [x] **Uncompressed binary trie** — control-plane optimized, O(prefix_len) writes
- [x] **Compressed Patricia trie** — data-plane optimized, O(k) reads, 4× memory savings
- [x] Generic engines — any engine can use any tree via `StandardEngine<T: RouteTree>`
- [x] Redis state bus — boot-load, cache hydration, Pub/Sub sync
- [x] IPv6 full support (Patricia trie path)
- [x] Python bindings via PyO3
- [x] Node.js bindings via N-API
- [x] gRPC service layer
- [x] Prometheus metrics integration
- [x] Lock-free CompressedTree (CAS-based node splitting)
- [] 🚧 (Adaptive Radix Tree) ART implementation in Go & Rust
   - [] Node4 (1-4 children, ~36 bytes)
   - [] Node16 (5-16 children, SIMD support)
   - [] Node48 (17-48 children, index array)
   - [] Node256 (49-256 children, direct array)
   - [] Auto-upgrade/downgrade logic
   - [] SIMD acceleration (x86 SSE / ARM NEON)
   - [] Zero-alloc lookups
   - [] Lock-free concurrency support
- [] Publish v1 of the rust crate to cargo.rs


## 🌲 Tree Type Selection

RadixIP provides two routing tree implementations. Choose based on your workload:

| | UncompressedTree | CompressedTree (Patricia) |
|---|---|---|
| **Write throughput** | ⚡ Fastest (no splitting) | 🟡 Moderate |
| **Read throughput** | 🟡 Good | ⚡ Fastest |
| **Memory (500K routes)** | ~4–8 MB | ~1–2 MB |
| **Best for** | BGP control plane | Packet forwarding / FIB |

```rust
// Rust — choose at construction time, zero runtime overhead
let control = StandardEngine::new(UncompressedTree::new(NodeVariant::Normal));
let fib     = StandardEngine::new(CompressedTree::new(NodeVariant::Normal));
```

```go
// Go — same API, different tree
control := NewStandardEngine(NewUncompressedTree(NodeNormal))
fib     := NewStandardEngine(NewCompressedTree(NodeNormal))
```

```go
// Or via EngineWrapper with compressed flag
controlPlane := NewEngineWrapperWithTree(EngineStandard, NodeNormal, false) // uncompressed
dataPlane    := NewEngineWrapperWithTree(EngineConcurrent, NodeAtomic, true)  // compressed
```

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the full design rationale, hybrid Redis architecture, and performance reference.


## 🤝 Contributing

We welcome contributions! Here's how to get started:

1. **Fork** the repository
2. **Create** a feature branch (`git checkout -b feature/amazing`)
3. **Commit** your changes (`git commit -m 'Add amazing feature'`)
4. **Push** to the branch (`git push origin feature/amazing`)
5. **Open** a Pull Request

**Guidelines**:
- Run benchmarks before/after to prove no regression
- Update documentation for any API changes
- Add tests for new functionality
- Keep performance as the #1 priority

## 📄Distribution Registries
Primary Source Engine: [github.com/Mwangi-Derrick/radixip](https://github.com/Mwangi-Derrick/radixip)

Infrastructure Target Mirror: [github.com/resplix/radixip](https://github.com/resplix/radixip)

## 📄 License

MIT License - See [LICENSE](LICENSE) for details.

---

Built with ❤️ by [Derrick Mwangi](https://github.com/Mwangi-Derrick) and the team at [Resplix](https://resplix.com).

High-performance IP subnet matching for modern infrastructure.

⭐ Star this repo if you find it useful!
