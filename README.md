# RadixIP

[![Go Reference](https://img.shields.io/badge/Go-1.27.2-00ADD8?logo=go)](https://github.com/Mwangi-Derrick/radixip)
[![Rust](https://img.shields.io/badge/Rust-1.99.1-orange?logo=rust)](https://github.com/Mwangi-Derrick/radixip)
[![CI](https://github.com/Mwangi-Derrick/radixip/actions/workflows/benchmarks.yml/badge.svg)](https://github.com/Mwangi-Derrick/radixip/actions/workflows/benchmarks.yml/badge.svg)
![Load Tests](https://github.com/Mwangi-Derrick/radixip/actions/workflows/load-test.yml/badge.svg)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/Mwangi-Derrick/radixip)](https://goreportcard.com/report/github.com/Mwangi-Derrick/radixip)

> **High-performance IP subnet routing, rate limiting, and infrastructure protection.**  
> Nanosecond memory lookups at Layer 7 paired with line-rate eBPF/XDP packet drops at Layer 2/3.

- **Rust ART:** 60.9 ns / lookup (SIMD-accelerated)
- **Go ART:** 72.6 ns / lookup (zero allocations on read path)
- **L2/L3 eBPF / XDP:** >200,000 pps dropped in software `veth`, line rate on physical NICs
- **Zero Heap Allocations:** On the ART read path
- **Instant Global Sync:** Local in-memory trees with distributed Redis Pub/Sub invalidation

---

## ⚡ Performance: L2/L3 eBPF/XDP vs. L7 Application Layer

RadixIP enforces security at two distinct boundaries:
1. **At the NIC Driver / Kernel (L2/L3 XDP)**: Drops packets before allocating socket buffers (`sk_buff`), negotiating TCP handshakes, or waking userspace.
2. **In Application Space (L7 gRPC / Middleware)**: Sub-microsecond CIDR lookups evaluating request context and client metadata.

Real measurements run in isolated Linux network namespaces (`veth` pair, 2-core x86_64 laptop):

| Enforcement Layer | Go (Cilium/C eBPF) | Rust (libbpf-rs/C eBPF) | Mechanism & Impact |
|---|---|---|---|
| **1. L7 Baseline (Allow)** | 8,865 req/s | 8,695 req/s | Full TCP + HTTP/2 frame parsing + gRPC dispatch |
| **2. L7 Blocklist (403)** | 9,179 req/s | 8,165 req/s | TCP handshake + HTTP/2 parsed + Userspace ART rejection |
| **3. L2/3 XDP (Generic / SKB)** | **217,350 pps** | **222,158 pps** | Kernel RX drop (`netif_receive_skb`), zero socket allocation |
| **4. L2/3 XDP (Native / DRV)** | **219,981 pps** | **203,058 pps** | Pre-allocation NIC driver hook, 0 application CPU usage |

> **~25x throughput leap**: Hostile subnets and botnet attacks are discarded in the kernel before your application or gRPC sidecar ever allocates a socket buffer.
>
> 🔬 **Reproduce locally:**
> ```bash
> sudo ./scripts/bench_xdp_vs_l7.sh all
> ```

---

## 🎯 What is RadixIP?

Standard hash maps cannot efficiently match IP addresses against arbitrary CIDR ranges (`/8`, `/16`, `/24`, `/32`, `/48`, `/64`, `/128`) without unrolling every possible prefix length.

RadixIP provides lock-free Adaptive Radix Tree (ART) and Patricia implementations with an L1 (in-process memory) + L2 (Redis look-aside) architecture:
- **Zero-allocation read path** for continuous high-throughput traffic.
- **Unified C eBPF kernel program** compiled with Clang and loaded via Cilium (`lib/go/xdp`) or `libbpf-rs` (`ebpf/xdp-loader`).
- **Drop-in middleware** for web frameworks and Kubernetes ingress sidecars.
- **Multi-language support** through C-ABI and Rust FFI.

**Use cases:** Edge DDoS mitigation · API gateways · database ACLs · geolocation subnet routing · botnet mitigation · distributed rate limiting.

> 📖 **[Read the Middleware Guide for Drop-in Integration](./docs/middleware.md)** (Gin, Axum, Actix, Fiber, Echo)

---

## 🎯 Quick Start Example

### 1. In-process subnet matching (Go)

```go
// 1. Insert blocked subnet
radixEngine.Insert("198.51.100.0/24", "blocked")

// 2. Incoming IP lookup: 60-70 ns, zero allocations
val, found := radixEngine.Match(netip.MustParseAddr("198.51.100.42"))
if found {
    // Drop or reject request
}
```

### 2. Multi-node distributed synchronization

```bash
docker compose up -d redis
```

Local instances serve reads directly from memory with zero network latency, while subscribing to Redis Pub/Sub for cross-node updates. For an end-to-end walkthrough, see [docs/guides/redis-cache-sync.md](./docs/guides/redis-cache-sync.md).

---

## 📚 Technical Documentation

In-depth architectural decisions, data structure design, and kernel mechanics:

| Guide | Description |
|---|---|
| 📖 [eBPF & XDP Packet Processing](./docs/guides/ebpf-xdp-integration.md) | LPM Trie map contract, driver hook modes, libbpf-rs loader, and Kubernetes pod integration |
| 📖 [Network Layers & Packet Flow](./docs/guides/network-layers-and-radixip.md) | OSI layers L1–L7, packet journey through Kubernetes CNI/veth, and sidecar inspection boundaries |
| 📖 [Radix Tree Design](./docs/guides/radix-tree-design.md) | Why Adaptive Radix Trees beat hash maps and traditional tries for CIDR routing |
| 📖 [Architecture](./docs/guides/architecture.md) | In-process L1 cache, Redis L2 synchronization |
| 📖 [Sharding](./docs/guides/sharding-architecture.md) | Sharding strategies |
| 📖 [Benchmark Methodology](./docs/guides/benchmark-methodology.md) | How latency and throughput are measured across Go, Rust, and XDP |
| 📖 [Go SIMD via Rust FFI](./docs/guides/go-simd-rust-ffi.md) | AVX2/NEON vector acceleration for 16-slot key scans |

---


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
- allocation-aware read path, with zero-allocation ART lookups in the Go CI benchmark
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

Representative CI results comparing three different tree families:

| Tree family | Rust implementation | Go implementation | What it measures |
|---|---|---|---|
| Binary trie | `NormalTrieNode` | `NormalTrieNode` | Uncompressed bit-by-bit trie traversal |
| Patricia / RadixNode tree | `NormalRadixNode` | `NormalRadixNode` | Compressed Patricia/radix traversal using RadixNode variants |
| Adaptive Radix Tree (ART) | `EngineVariant::ART` | `NewARTEngineAdapter` | Adaptive node-size tree optimized for dense lookup paths |

| Runtime | Structure | Insert batch | Insert / prefix | Concurrent lookup / op | Source benchmark |
|---|---|---:|---:|---:|---|
| Rust | Binary trie (`NormalTrieNode`) | 25,275,481 ns / 5k | 5,055 ns | 728.6 ns | `bench_insert`, `bench_concurrent_lookup_uncompressed` |
| Rust | Patricia/RadixNode tree (`NormalRadixNode`) | 11,754,339 ns / 5k | 2,351 ns | 177.7 ns | `bench_insert`, `bench_concurrent_lookup_compressed` |
| Rust | ART (`EngineVariant::ART`) | 761,077 ns / 5k | 152.2 ns | 60.9 ns | `bench_insert`, `bench_concurrent_lookup_art` |
| Go | Binary trie (`NormalTrieNode`) | 49,013,688 ns / 5k | 9,803 ns | 118.7 ns | `BenchmarkInsert_Uncompressed_5k_Normal`, `BenchmarkConcurrent_Lookup_Uncompressed_Normal` |
| Go | Patricia/RadixNode tree (`NormalRadixNode`) | 12,021,442 ns / 5k | 2,404 ns | 223.4 ns | `BenchmarkInsert_Compressed_5k_Normal`, `BenchmarkConcurrent_Lookup_Compressed_Normal` |
| Go | ART (`NewARTEngineAdapter`) | 2,393,602 ns / 10k | 239.4 ns | 72.6 ns | `BenchmarkInsert_ART_10k`, `BenchmarkConcurrent_Lookup_ART_50k` |

Sequential Go lookup batches from `lib/go/engine_test.go`:

| Structure | Hit workload | Hit / lookup | Miss workload | Miss / lookup | Allocations |
|---|---:|---:|---:|---:|---:|
| Binary trie (`NormalTrieNode`) | 1,717,544 ns / 25k | 68.7 ns | 1,726,589 ns / 25k | 69.1 ns | 24 B/op, 1 alloc/lookup |
| Patricia/RadixNode tree (`NormalRadixNode`) | 8,349,666 ns / 50k | 167.0 ns | 3,077,318 ns / 50k | 61.5 ns | 24 B/op, 1 alloc/lookup |
| ART (`NewARTEngineAdapter`) | 1,347,823 ns / 50k | 27.0 ns | 1,041,880 ns / 50k | 20.8 ns | 0 B/op, 0 allocs |

Key takeaways from this CI run:

- Rust ART is the fastest concurrent lookup path measured here at **60.9 ns/lookup**.
- Go ART is close at **72.6 ns/lookup** and has **0 B/op** on ART lookup benchmarks.
- Patricia/RadixNode-tree insert is about **2.1x faster than Rust binary trie** for the normal Rust nodes, and ART insert is about **15.4x faster than the Rust Patricia/RadixNode tree** in the measured insert workload.

See the CI artifacts for complete benchmark logs and hardware information.

---

## 🎯 Why It Matters

Algorithmic complexity is only part of the performance story.

On modern processors, memory access patterns frequently dominate execution time.

A well-designed data structure not only performs fewer operations-it performs them in a way that works *with* the processor's cache hierarchy rather than against it.

For networking, routing, and access-control workloads, that difference is often more important than asymptotic complexity alone.

## 🏗️ Architecture

### 🔄 The Hybrid L1/L2 Pipeline

Redis is completely removed from the critical lookup path. Every validation runs locally inside the application's process memory.

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
Protect databases (PostgreSQL, MySQL, MongoDB) by validating client IPs against dynamic whitelists at the proxy layer-before expensive authentication handshakes.

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
- **Local memory-speed lookups** → no external API call on cache hit
- **Automatic TTL** → fresh data when needed


### 6. Rate Limiting & Abuse Prevention ⚡
Protect APIs from brute force attacks, scraping, and abuse. Define per-IP, per-API-key, or per-route limits with sliding window, token bucket, or fixed window algorithms. Auto-flag and ban IPs that exceed thresholds.

**The Result**: Stop bad actors before they reach your application.

---


## 📊 Performance Benchmarks

The current headline benchmark is the CI-measured concurrent lookup path:

| Runtime | Fastest measured structure | Latency / lookup | Throughput | Allocations |
|---|---|---:|---:|---:|
| **Rust (`radixip-rs`)** | ART | 60.9 ns | 16.4M ops/sec | not reported by Criterion |
| **Go (`radixip-go`)** | ART | 72.6 ns | 13.8M ops/sec | 0 B/op |
| **Rust (`radixip-rs`)** | Patricia / RadixNode tree | 177.7 ns | 5.6M ops/sec | not reported by Criterion |
| **Go (`radixip-go`)** | Patricia / RadixNode tree | 223.4 ns | 4.5M ops/sec | 24 B/op, 1 alloc/op |

The fastest structure is not the same as the most general-purpose structure for every workload. The binary trie and Patricia/RadixNode tree implementations remain useful as simple, predictable LPM baselines; ART is the current high-throughput read path.

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
import (
    "net"

    radixip "github.com/Mwangi-Derrick/radixip/lib/go/engine"
)

func main() {
    engine := radixip.NewEngineWrapperWithTree(
        radixip.EngineConcurrent,
        radixip.NormalRadixNode,
        true,
    )

    _, prefix, _ := net.ParseCIDR("192.168.0.0/16")
    _ = engine.Insert(prefix, radixip.Metadata{
        Value: "allow",
        Attributes: map[string]string{
            "region": "Nairobi",
            "isp":    "Safaricom",
        },
    })

    result := engine.Lookup(net.ParseIP("192.168.1.100"))
    // result != nil; result.Value == "allow"
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
go get github.com/Mwangi-Derrick/radixip/lib/go/engine
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
curl -LO https://github.com/Mwangi-Derrick/radixip/releases/latest/download/libradixip.so
```


## 🔄 CI/CD Pipeline

Every commit to `main` triggers our continuous benchmarking pipeline:

1. **Build** the Rust SIMD FFI shared library used by Go's SIMD CGo path
2. **Run** Go benchmarks (`go test -run=NONE -bench=. -benchmem -count=10 -v -tags simd_cgo ./...`)
3. **Run** Rust benchmarks (`cargo bench --bench lookup_bench -- --output-format bencher`)
4. **Upload** raw performance logs as artifacts
5. **Fail** if performance regresses beyond 5%

**View results**: [GitHub Actions tab](https://github.com/Mwangi-Derrick/radixip/actions)

**Download artifacts**: Raw benchmark logs available for auditing.

## 🌲 Tree Type Selection

RadixIP provides three routing tree implementations. Choose based on your workload:

| | UncompressedTree (Binary Trie) | CompressedTree (Binary Patricia Tree) | Adaptive Radix Tree (ART)  |
|---|---|---|---|
| **Write throughput** | ⚡ Fastest (no splitting) | 🟡 Moderate | 🟡 Moderate (node upgrades) |
| **Read throughput** | 🟡 Good | ⚡ Fast | ⚡⚡ Fastest (SIMD / cache-aligned) |
| **Memory (500K routes)** | ~4–8 MB | ~1–2 MB | ~1.5–3 MB (dynamic node sizes) |
| **Best for** | BGP control plane | Packet forwarding / FIB | Extremely high-performance routing |

```rust
// Rust - choose at construction time, zero runtime overhead
let control = StandardEngine::new(UncompressedTree::new(NodeVariant::NormalTrieNode));
let fib     = StandardEngine::new(CompressedTree::new(NodeVariant::NormalRadixNode));
```

```go
// Go - same API, different tree
control := NewStandardEngine(NewUncompressedTree(NodeNormal))
fib     := NewStandardEngine(NewCompressedTree(NodeNormal))
```

```go
// Or via EngineWrapper with compressed flag
controlPlane := NewEngineWrapperWithTree(EngineStandard, NormalTrieNode, false) // uncompressed
dataPlane    := NewEngineWrapperWithTree(EngineConcurrent, AtomicRadixNode, true)  // compressed
```

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the full design rationale, hybrid Redis architecture, and performance reference.


## 🗺️ Roadmap

### Core Engine

- [x] Go implementation with lock-free reads
- [x] Rust port with zero-cost abstractions  
- [x] C-FFI bindings for multi-language support
- [x] CI benchmarking pipeline
- [x] **Uncompressed binary trie** - control-plane optimized, O(prefix_len) writes
- [x] **Compressed Patricia trie** - data-plane optimized, O(k) reads, 4× memory savings
- [x] Generic engines - any engine can use any tree via `StandardEngine<T: RouteTree>`
- [x] Redis state bus - boot-load, cache hydration, Pub/Sub sync
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

### Multi-language bindings

- [x] Publish v1.0.0 release of the rust crate to crates.io
- [x] Publish the NAPI-RS node bindings to npm
- [x] Publish the PyO3 bindings to PYPI

### Configuration Engine
- [x] YAML/TOML config parser
- [x] Hot-reload without restart
- [x] Per-route overrides
- [x] Validation + schema

### Protection Layer
- [x] Web framework middleware (Express, Gin, Axum, Actix, FastAPI, Django, Fiber, etc.)
- [x] Distributed rate limiting (Token Bucket, Sliding Window, Fixed Window)
- [x] Configurable IP flagging and auto-banning
- [x] Unified configuration format

### Kubernetes & Operations 🔨
- [x] Redis Pub/Sub for distributed sync
- [x] Kubernetes Operator for automated deployment
- [ ] Redis HA failover support
- [x] Prometheus metrics integration
- [x] gRPC service layer
- [ ] Helm charts for one-command install

### Hardware Acceleration (NEW) 🔨
- [ ] **FPGA acceleration for LPM lookups** - Offload critical path to hardware (projected 2-5 ns lookups, parallel processing)
- [x] **Kernel bypass for gRPC sidecars** - eBPF for Linux networking stack to handle L2,L3 and L4 routing decisions and AF_XDP for zero-copy packet I/O (5x throughput improvement) across platforms
- [ ] **Runtime dispatch in Rust** - Transparent fallback between software and FPGA backends
- [ ] **Custom RadixIP FPGA appliance** - Pre-built bitstreams for popular FPGA boards (Tang Nano, Artix, Kintex)



## 🧪 Validation & Load Testing

RadixIP includes a comprehensive validation suite to ensure the middleware, gRPC interceptors, and core engine behave correctly under sustained load.

### Kitchen Sink Applications

We maintain five unified test applications that spin up all supported frameworks concurrently. They share a single, in-memory RadixIP `PolicyEngine` (including the Radix Tree, auto-ban sweepers, and Token Bucket limiters):

- **Go Kitchen Sink** (`cmd/kitchen-sink-go`): Runs Gin (`:8081`), Echo (`:8082`), Fiber (`:8083`), Chi (`:8084`), Net/Http (`:8085`), and gRPC (`:50051`) sharing one engine.
- **Rust Kitchen Sink** (`cmd/kitchen-sink-rust`): Runs Axum (`:9081`), Actix-Web (`:9082`), and Tonic gRPC (`:50052`) sharing one engine.
- **Node Kitchen Sink** (`cmd/kitchen-sink-node`): Run Express.js (`:8091`), Fastify(`:8092`), Next.js (`:8094`) sharing one engine.
- **Tanstack Kitchen Sink** (`cmd/kitchen-sink-tanstack`): Run Tanstack Start
(`:8097`).
- **Python Kitchen Sink** (`cmd/kitchen-sink-python`): Run Fast API (`:8093`),
Django (`:8095`) and Flask (`:8096`) sharing one engine.

### E2E Orchestrator Test Suite

The `cmd/e2e-orchestrator` directory contains a Go application that completely automates load testing across all supported languages and frameworks (Rust, Go, Node.js, Python). It internally downloads [Vegeta](https://github.com/tsenart/vegeta) and [ghz](https://ghz.sh/), manages server subprocesses, and executes a three-phase validation.

**Running the E2E Orchestrator**:
```bash
go run ./cmd/e2e-orchestrator --results-dir ./test-results
```

This master orchestrator performs a three-phase validation:

#### Phase 1: Route-Trie Specific Rate Limits
Tests the configuration engine's segment-based Radix Trie. For example, if `radixip.yaml` defines:
```yaml
rate_limit_routes:
  enabled: true
  routes:
  - path: "/api/v1/auth"
    methods: ["POST"]
    rate_limit: { capacity: 5, refill_rate: 1 }
  - path: "/api/v1/*"
    methods: ["GET"]
    rate_limit: { capacity: 1000, refill_rate: 100 }
```
Phase 1 no longer uses vegeta. It calls runHTTPBatch with a deterministic batch of 9 requests per route — enough to trigger 429s on the auth endpoint (capacity 5) while staying below the 5-violation auto-ban threshold. This isolates the route-trie policy from the auto-ban lifecycle, which Phase 2 owns exclusively.

Each framework gets a distinct X-Forwarded-For IP per route, and the auth IPs (203.0.113.x) and public IPs (203.0.114.x) are on different /24 blocks. This guarantees that a rate-limit violation on one route cannot contaminate the other, even though the policy engine shares state across routes and frameworks.

#### Phase 2: Auto-Ban & Sweeper Verification
Tests the `AutoBanTracker`. This test calls vegeta. It attacks each framework at high RPS using a single IP. 
1. The first requests succeed up to the global rate-limit capacity.
2. The next requests return `429 Too Many Requests`.
3. Once the IP hits the `violation_threshold` (e.g., 5 violations), the engine injects it into the blocklist.
4. All subsequent requests return `403 Forbidden` instantly.
5. The test sleeps for the configured ban duration (e.g., 35s), then verifies that the background sweeper correctly evicted the IP, restoring `200 OK` access.

#### Phase 3: gRPC Auto-Ban & Sweeper Verification

The orchestrator natively tests both gRPC servers:
- Go gRPC on `localhost:50051`
- Rust/Tonic gRPC on `localhost:50052`

The test calls `radixip.v1.RadixService/Lookup` with `x-forwarded-for` metadata using `ghz`. It asserts:
- `PermissionDenied` responses from both Go and Rust after auto-ban activation
- The Go and Rust bans lifted successfully after the 35-second sweeper wait
- A final gRPC request succeeding after the ban expired

For a local run, simply execute:
```bash
go run ./cmd/e2e-orchestrator --results-dir ./test-results
```
The orchestrator manages dependencies automatically, requiring only a modern Go toolchain and cargo/npm/python to build the sink apps.

### Spoof Proxy & Attack Simulation
For advanced A/B testing and distributed IP spoofing, you can run `scripts/spoof_proxy` alongside load generation tools to simulate thousands of distinct, malicious IPs hitting the edge simultaneously.


## 🤝 Contributing

We welcome contributions! Here's how to get started:

1. **Fork** the repository
2. **Create** a feature branch (`git checkout -b feature/amazing`)
3. **Commit** your changes (`git commit -m 'Add amazing feature'`)
4. **Push** to the branch (`git push origin feature/amazing`)
5. **Open** a Pull Request

**Guidelines**:
- Update documentation for any API changes
- Add tests for new functionality
- Keep performance as the #1 priority

## 📄Distribution Registries
Primary Source Engine: [github.com/Mwangi-Derrick/radixip](https://github.com/Mwangi-Derrick/radixip)

Infrastructure Target Mirror: [github.com/resplix/radixip](https://github.com/resplix/radixip)

## 📄 License

MIT License - See [LICENSE](LICENSE) for details.

---

Built with ❤️. High-performance IP subnet matching for modern infrastructure.
