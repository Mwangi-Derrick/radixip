# RadixIP

[![Go Reference](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://github.com/Mwangi-Derrick/radixip)
[![Rust](https://img.shields.io/badge/Rust-1.95-orange?logo=rust)](https://github.com/Mwangi-Derrick/radixip)
[![CI](https://github.com/Mwangi-Derrick/radixip/actions/workflows/bench.yml/badge.svg)](https://github.com/Mwangi-Derrick/radixip/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/Mwangi-Derrick/radixip)](https://goreportcard.com/report/github.com/Mwangi-Derrick/radixip)

**High-performance IP subnet caching engine with zero-allocation LPM lookups**

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

# 🧠 Why Radix Trees?

IP subnet matching is fundamentally different from exact-key lookups.

Given an address like:

```
192.168.1.42
```

the engine must determine the **longest matching prefix**:

```
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

```
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

```
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

## 🧠 Why Radix Trees?

Radix trees win for IP subnet matching because of **spatial locality**:

1. **Sequential memory access** → CPU prefetcher works
2. **No hashing overhead** → pure bit operations
3. **Natural LPM support** → prefix matching built-in
4. **Flat arrays** → no pointer chasing, all nodes in one cache line

**Result**: ~45ns lookups in Go, ~12ns in Rust.

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


### **CI Pipeline**

## 🔄 CI/CD Pipeline

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
- [ ] IPv6 support (planned)
- [ ] Python bindings via PyO3
- [ ] Node.js bindings via N-API
- [ ] gRPC service layer
- [ ] Prometheus metrics integration

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

## 📦 Where to Find This Project

- **Primary**: [github.com/Mwangi-Derrick/radixip](https://github.com/Mwangi-Derrick/radixip)
- **Mirror**: [github.com/resplix/radixip](https://github.com/resplix/radixip)

## 📄 License

MIT License - See [LICENSE](LICENSE) for details.

---

Built with ❤️ by [Derrick Mwangi](https://github.com/Mwangi-Derrick) and [Resplix](https://resplix.com)High-performance IP subnet matching for modern infrastructure.

⭐ Star this repo if you find it useful!
