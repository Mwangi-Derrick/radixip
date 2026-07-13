# ⚡ RadixIP

[![Go Reference](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://github.com/Mwangi-Derrick/radixip)
[![Rust](https://img.shields.io/badge/Rust-1.70+-orange?logo=rust)](https://github.com/Mwangi-Derrick/radixip)
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

## 🏗️ Architecture
┌─────────────────────────────────────────────────────────────┐
│ API Gateway / Proxy Layer │
└──────────────────────────┬──────────────────────────────────┘
│
▼
┌─────────────────────────────────────────────────────────────┐
│ L1: RadixIP Engine │
│ ┌─────────────┐ ┌─────────────┐ ┌────────────────────┐ │
│ │ Go Module │ │ Rust Crate │ │ C-FFI Bindings │ │
│ │ (radixip- │ │ (radixip- │ │ (Python, Node.js, │ │
│ │ go/) │ │ rs/) │ │ C/C++, etc.) │ │
│ └─────────────┘ └─────────────┘ └────────────────────┘ │
│ │
│ Lock-free Binary Radix Tree (Zero allocations on read) │
└──────────────────────────┬──────────────────────────────────┘
│
▼
┌─────────────────────────────────────────────────────────────┐
│ L2: Redis Cache │
│ ┌────────────────────────────────────────────────────────┐ │
│ │ • Subnet → Metadata mappings │ │
│ │ • Pub/Sub for instant invalidation │ │
│ │ • Persistent backing store │ │
│ └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘

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
```markdown
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

## 📄 License

MIT License - See [LICENSE](LICENSE) for details.

---

Built with ❤️ by [Derrick Mwangi](https://github.com/Mwangi-Derrick)