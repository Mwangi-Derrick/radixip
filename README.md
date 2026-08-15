# RadixIP

[![Go Reference](https://img.shields.io/badge/Go-1.26.1-00ADD8?logo=go)](https://github.com/Mwangi-Derrick/radixip)
[![Rust](https://img.shields.io/badge/Rust-1.97.1-orange?logo=rust)](https://github.com/Mwangi-Derrick/radixip)
[![CI](https://github.com/Mwangi-Derrick/radixip/actions/workflows/bench.yml/badge.svg)](https://github.com/Mwangi-Derrick/radixip/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/Mwangi-Derrick/radixip)](https://goreportcard.com/report/github.com/Mwangi-Derrick/radixip)

> **The missing link between network security, DDoS mitigation, and geolocation caching.**
>
> RadixIP gives you IP filtering at memory speed: **72.6 ns/lookup in Go ART** and **60.9 ns/lookup in Rust ART** on the current CI benchmark runner.
> Block attacks, secure databases, and save **$3.6M/year** on geolocation APIs.

> **Go ART:** 72.6 ns/lookup - **Rust ART:** 60.9 ns/lookup - **Binary radix:** 177.7-223.4 ns/lookup - **FFI:** native SIMD support from Go

## 🎯 What is RadixIP?

RadixIP is a production-grade IP subnet caching engine that solves a critical infrastructure problem:

**The Problem**: Standard hash maps can't efficiently match IPs against dynamic CIDR blocks (`/8`, `/16`, `/24`, `/32`) at scale. Database ACLs, API gateways, and edge proxies need sub-microsecond lookups with zero GC pressure.

**The Solution**: A lock-free binary radix tree with L1 (in-memory) + L2 (Redis look-aside) architecture, enabling:
- **72.6 ns** concurrent LPM lookups in Go ART
- **60.9 ns** concurrent LPM lookups in Rust ART
- **177.7 ns** Rust binary Patricia/radix lookups and **223.4 ns** Go binary Patricia/radix lookups
- **Zero heap allocations** on the Go ART read path; allocation behavior varies by engine variant
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
| [Go SIMD via Rust FFI](./docs/guides/go-simd-rust-ffi.md) | Why the Go ART Node16 uses a Rust-backed SIMD shared library instead of Go's experimental native SIMD |

## Reading order

If you're new to networking data structures, read in this order:

1. [How Routers Work](./docs/guides/how-routers-work.md) — the motivating context
2. [Longest Prefix Match](./docs/guides/longest-prefix-match.md) — the algorithm
3. [Radix Tree Design](./docs/guides/radix-tree-design.md) — the data structure that makes it fast
4. [IPv4 vs IPv6](./docs/guides/ipv4-vs-ipv6.md) — how it changes across protocols
5. [Cache Locality](./docs/guides/cache-locality.md) — why the implementation is shaped the way it is
6. [Architecture](./docs/guides/architecture.md) — how it's wired into a real system
7. [Benchmark Methodology](./docs/guides/benchmark-methodology.md) — how to verify all of the above


## ⚡ SIMD Acceleration (Node16)

The Go ART Node16 node accelerates its 16-slot key scan using SIMD via a
thin CGo bridge to a Rust shared library (`libnode16_simd_ffi`).

| Architecture | Instruction set | How |
|---|---|---|
| x86_64 | **AVX2** → **SSE4.1** → scalar | Runtime dispatch in Rust |
| aarch64 | **NEON** | Compile-time (mandatory baseline) |
| other | Scalar | Pure-Rust fallback |

This approach was chosen because Go 1.26's native SIMD is experimental and
limited to x86_64.  The Rust path gives full multi-arch coverage today.

```bash
# Build the shared library, then compile Go with SIMD active:
make build-go-simd

# Run Go ART tests with SIMD:
make test-go-simd
```

> 📖 Full rationale, build instructions, and memory-safety notes:
> **[docs/guides/go-simd-rust-ffi.md](./docs/guides/go-simd-rust-ffi.md)**

---

## 🧠 Design Goals

RadixIP is designed around a few core principles:

- ⚡ Zero-allocation ART read path, with allocation behavior tracked per engine variant
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
| Adaptive Radix Tree (ART)  | ✅ | ✅ | Very Low | Dynamic node sizes, SIMD/cache-friendly, ultra-fast reads |

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

Benchmarks are executed by the GitHub Actions `Benchmarks` workflow on `ubuntu-latest`.

Current CI runner details from the Go benchmark log:

- OS/arch: `linux/amd64`
- CPU: `INTEL(R) XEON(R) PLATINUM 8573C`
- Go command: `go test -run=NONE -bench=. -benchmem -count=10 -v -tags simd_cgo ./...`
- Rust command: `cargo bench --bench lookup_bench -- --output-format bencher`

The benchmark source mapping is:

| Runtime | Source | Benchmark functions |
|---|---|---|
| Rust | [`lib/rust/benches/lookup_bench.rs`](./lib/rust/benches/lookup_bench.rs) | `bench_insert`, `bench_concurrent_lookup_uncompressed`, `bench_concurrent_lookup_compressed`, `bench_concurrent_lookup_art` |
| Go engine | [`lib/go/engine_test.go`](./lib/go/engine_test.go) | `BenchmarkInsert_*`, `BenchmarkLookup_Hit_*`, `BenchmarkLookup_Miss_*`, `BenchmarkConcurrent_Lookup_*`, `Benchmark*_ART_*` |
| Go ART tree | [`lib/go/art/tree_test.go`](./lib/go/art/tree_test.go) | `BenchmarkTree_Insert`, `BenchmarkTree_Match_Hit`, `BenchmarkTree_Match_Miss` |
| Go integration tests | [`lib/go/tests/engine_test.go`](./lib/go/tests/engine_test.go) | Functional tests only; no benchmark functions |

Rust Criterion reports whole-batch times for these benchmarks. Insert results below divide `ns/iter` by `5,000` prefixes, and Rust concurrent lookup results divide `ns/iter` by `4 * 25,000 = 100,000` lookups. Go batched hit/miss lookup results divide `ns/op` by the loop size in the benchmark source; Go `RunParallel` concurrent results are already per lookup.

Results should be interpreted as measurements for the tested hardware and compiler versions rather than universal performance guarantees.

---

## 📊 CI Benchmark Results

Representative CI results comparing the normal binary trie, normal binary Patricia/radix tree, and ART implementations:

| Runtime | Structure | Insert batch | Insert / prefix | Concurrent lookup / op | Source benchmark |
|---|---|---:|---:|---:|---|
| Rust | Binary trie (`NormalTrieNode`) | 25,275,481 ns / 5k | 5,055 ns | 728.6 ns | `bench_insert`, `bench_concurrent_lookup_uncompressed` |
| Rust | Binary radix (`NormalRadixNode`) | 11,754,339 ns / 5k | 2,351 ns | 177.7 ns | `bench_insert`, `bench_concurrent_lookup_compressed` |
| Rust | ART (`EngineVariant::ART`) | 761,077 ns / 5k | 152.2 ns | 60.9 ns | `bench_insert`, `bench_concurrent_lookup_art` |
| Go | Binary trie (`NormalTrieNode`) | 49,013,688 ns / 5k | 9,803 ns | 118.7 ns | `BenchmarkInsert_Uncompressed_5k_Normal`, `BenchmarkConcurrent_Lookup_Uncompressed_Normal` |
| Go | Binary radix (`NormalRadixNode`) | 12,021,442 ns / 5k | 2,404 ns | 223.4 ns | `BenchmarkInsert_Compressed_5k_Normal`, `BenchmarkConcurrent_Lookup_Compressed_Normal` |
| Go | ART (`NewARTEngineAdapter`) | 2,393,602 ns / 10k | 239.4 ns | 72.6 ns | `BenchmarkInsert_ART_10k`, `BenchmarkConcurrent_Lookup_ART_50k` |

Sequential Go lookup batches from `lib/go/engine_test.go`:

| Structure | Hit workload | Hit / lookup | Miss workload | Miss / lookup | Allocations |
|---|---:|---:|---:|---:|---:|
| Binary trie (`NormalTrieNode`) | 1,717,544 ns / 25k | 68.7 ns | 1,726,589 ns / 25k | 69.1 ns | 24 B/op, 1 alloc/lookup |
| Binary radix (`NormalRadixNode`) | 8,349,666 ns / 50k | 167.0 ns | 3,077,318 ns / 50k | 61.5 ns | 24 B/op, 1 alloc/lookup |
| ART (`NewARTEngineAdapter`) | 1,347,823 ns / 50k | 27.0 ns | 1,041,880 ns / 50k | 20.8 ns | 0 B/op, 0 allocs |

Key takeaways from this CI run:

- Rust ART is the fastest concurrent lookup path measured here at **60.9 ns/lookup**.
- Go ART is close at **72.6 ns/lookup** and has **0 B/op** on ART lookup benchmarks.
- Binary Patricia/radix insert is about **2.1x faster than Rust binary trie** for the normal Rust nodes, and ART insert is about **15.4x faster than Rust binary radix** in the measured insert workload.
- The old `~45 ns Go` and `~12 ns Rust` headline numbers are no longer claimed by this README; the tables above are derived directly from the current CI logs.

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
│  │ • Allocation-aware read path                                                                 │  │
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
[Incoming Request] ──> L1: Local Radix (memory-speed, FREE) ──[Hit]──> Return Metadata
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

1. **L1: RadixIP** (local memory-speed lookup, FREE)
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

The current headline benchmark is the CI-measured concurrent lookup path:

| Runtime | Fastest measured structure | Latency / lookup | Throughput | Allocations |
|---|---|---:|---:|---:|
| **Rust (`radixip-rs`)** | ART | 60.9 ns | 16.4M ops/sec | not reported by Criterion |
| **Go (`radixip-go`)** | ART | 72.6 ns | 13.8M ops/sec | 0 B/op |
| **Rust (`radixip-rs`)** | Binary Patricia/radix | 177.7 ns | 5.6M ops/sec | not reported by Criterion |
| **Go (`radixip-go`)** | Binary Patricia/radix | 223.4 ns | 4.5M ops/sec | 24 B/op, 1 alloc/op |

The fastest structure is not the same as the most general-purpose structure for every workload. The binary trie and binary Patricia/radix implementations remain useful as simple, predictable LPM baselines; ART is the current high-throughput read path.

**Why ART is leading in this run**:
- compact adaptive node sizes
- fewer traversal steps for dense key ranges
- SIMD-backed Node16 acceleration in the Go path
- zero-allocation Go ART lookups

**Audit the raw benchmark logs**: [Download from CI artifacts](https://github.com/Mwangi-Derrick/radixip/actions)

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
cd lib/go
go test -run=NONE -bench=. -benchmem -count=10 -v -tags simd_cgo ./...
```
Run Rust benchmarks
```bash
cd lib/rust
cargo bench --bench lookup_bench -- --output-format bencher
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

1. **Build** the Rust SIMD FFI shared library used by Go's SIMD CGo path
2. **Run** Go benchmarks (`go test -run=NONE -bench=. -benchmem -count=10 -v -tags simd_cgo ./...`)
3. **Run** Rust benchmarks (`cargo bench --bench lookup_bench -- --output-format bencher`)
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
- [x]  (Adaptive Radix Tree) ART implementation in Go & Rust
   - [x] Node4 (1-4 children, ~36 bytes)
   - [x] Node16 (5-16 children, SIMD support)
   - [x] Node48 (17-48 children, index array)
   - [x] Node256 (49-256 children, direct array)
   - [x] Auto-upgrade/downgrade logic
   - [x] SIMD acceleration (x86 SSE / ARM NEON)
   - [x] Zero-alloc lookups
   - [x] Lock-free concurrency support
- [] Publish v1 of the rust crate to cargo.rs
- [] Publish the NAPI-RS node bindings to npm
- [] Publish the PyO3 bindings to PYPI


## 🌲 Tree Type Selection

RadixIP provides three routing tree implementations. Choose based on your workload:

| | UncompressedTree (Binary Trie) | CompressedTree (Binary Patricia Tree) | Adaptive Radix Tree (ART)  |
|---|---|---|---|
| **Write throughput** | ⚡ Fastest (no splitting) | 🟡 Moderate | 🟡 Moderate (node upgrades) |
| **Read throughput** | 🟡 Good | ⚡ Fast | ⚡⚡ Fastest (SIMD / cache-aligned) |
| **Memory (500K routes)** | ~4–8 MB | ~1–2 MB | ~1.5–3 MB (dynamic node sizes) |
| **Best for** | BGP control plane | Packet forwarding / FIB | Extremely high-performance routing |

```rust
// Rust — choose at construction time, zero runtime overhead
let control = StandardEngine::new(UncompressedTree::new(NodeVariant::NormalTrieNode));
let fib     = StandardEngine::new(CompressedTree::new(NodeVariant::NormalRadixNode));
```

```go
// Go — same API, different tree
control := NewStandardEngine(NewUncompressedTree(NodeNormal))
fib     := NewStandardEngine(NewCompressedTree(NodeNormal))
```

```go
// Or via EngineWrapper with compressed flag
controlPlane := NewEngineWrapperWithTree(EngineStandard, NormalTrieNode, false) // uncompressed
dataPlane    := NewEngineWrapperWithTree(EngineConcurrent, AtomicRadixNode, true)  // compressed
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
