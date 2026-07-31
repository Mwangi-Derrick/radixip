# Contributing to RadixIP

Thank you for your interest in contributing to RadixIP! This project aims to provide carrier-grade, sub-microsecond IP subnet caching primitives for high-throughput infrastructure.

Because RadixIP sits directly in the critical read path of API gateways, proxies, and database firewalls, we maintain strict standards regarding execution speed, memory allocations, and multi-threaded synchronization.

---

## 📜 Our Core Principles

1. **Performance First:** Algorithmic optimization is only half the battle. Every patch must respect the CPU cache hierarchy and avoid unnecessary pointer indirection.
2. **Zero Allocations:** The lookup path for both Go and Rust must maintain `0` heap allocations. Primitives should live on the stack or use pre-allocated, flat layouts.
3. **Thread Safety Without Bottlenecks:** We prioritize lock-free reading patterns (`sync/atomic` in Go, `ArcSwap` or atomics in Rust). Heavy global mutexes on lookup paths will not be accepted.

---

## 🛠️ Development Setup

Ensure you have the required toolchains installed for both ecosystems:
- **Go:** 1.26.1+
- **Rust:** Stable 1.97.1+ (with `cargo`)
- **Python:** 3.10+ (for generating mock dataset matrices)

### 1. Clone & Initialize Dataset

```bash
git clone https://github.com/Mwangi-Derrick/radixip.git
cd radixip

# Populate the local high-entropy evaluation matrix
python scripts/generate_mock_data.py --subnets 10000 --lookups 100000
```

### 2. Running Local Tests & Verification

Before writing code, ensure the baseline passes across all modules.

**Go Module (radixip-go):**

```bash
cd radixip-go
go test -v ./...
```

**Rust Crate (radixip-rs):**

```bash
cd radixip-rs
cargo test
```

---

## 📈 The Benchmark Requirement (Mandatory)

We enforce a strict continuous benchmarking pipeline. If you submit a feature or an optimization, you must include before-and-after benchmarks in your Pull Request. Any patch that introduces a performance regression > 3% will be automatically blocked by CI.

### How to Run and Log Benchmarks Locally

**For Go changes:**

```bash
cd radixip-go
go test -bench=. -benchmem ./... > go_new_bench.txt
```

Verify that `allocs/op` stays strictly at `0` for lookups.

**For Rust changes:**

```bash
cd radixip-rs
cargo bench -- --output-format bencher > rust_new_bench.txt
```

---

## 📥 Pull Request Guidelines

To ensure a smooth review process, please structure your submissions as follows:

### Branch Naming

Use descriptive branch paths:
- `perf/compress-nodes`
- `fix/go-atomic-race`
- `feat/ipv6-support`

### Atomic Commits

Keep commits isolated and focused on single logical changes.

### PR Description Template

```markdown
**What changed?**  
Briefly detail the structural alterations.

**Why this path?**  
Explain the low-level reasoning (e.g., "Swapped out pointer array for inline vector to reduce L2 cache misses").

**Benchmark Delta:**  
Paste your local comparative results matching this format:

```
Baseline (Main):  45.2 ns/op
Proposed Patch:   41.1 ns/op (~9% improvement)
```
```

---

## 🌳 Code Style & Architecture Constraints

### Go Layer

```go
// ✅ GOOD: Use netip.Addr (24 bytes, comparable as integers, no dynamic backing array)
import "net/netip"

func lookup(ip netip.Addr) *GeoData {
    // May still escape if stored in an interface/map, but memory footprint is smaller.
    return radixTree.Lookup(ip)
}

// ❌ BETTER TO AVOID: net.IP (slice header + backing array, slower comparisons)
import "net"

func lookup(ip net.IP) *GeoData {
    // Backing array may or may not allocate depending on where the IP came from.
    return radixTree.Lookup(ip)
}
```

**Key Rules:**
- Leverage `net/netip.Addr` over the older, allocation-heavy `net.IP`.
- Any modification to the tree layout must use atomic pointer swapping (`sync/atomic.Pointer`) to guarantee entirely non-blocking concurrent reads.

```go
// ✅ CORRECT: Atomic pointer swap for lock-free updates
var root atomic.Pointer[RadixNode]

func UpdateTree(newRoot *RadixNode) {
    root.Store(newRoot)  // Atomic, non-blocking
}

func Lookup(ip netip.Addr) *GeoData {
    node := root.Load()  // Lock-free read
    return node.Search(ip)
}
```

### Rust Layer

```rust
// ✅ GOOD: Cache-line aligned to prevent false sharing
#[repr(align(64))]
pub struct RadixNode {
    prefix: Ipv6Addr,
    children: [Option<Box<RadixNode>>; 2],
    data: Option<GeoData>,
}

// ✅ GOOD: Atomic reads with ArcSwap for lock-free concurrency
use arc_swap::ArcSwap;

pub struct RadixTree {
    root: ArcSwap<RadixNode>,
}

impl RadixTree {
    pub fn lookup(&self, ip: Ipv6Addr) -> Option<&GeoData> {
        let node = self.root.load();  // Lock-free read
        node.search(ip)
    }
}
```

**Key Rules:**
- Keep structures cache-line aligned (`#[repr(align(64))]`) where practical to mitigate false-sharing across worker threads.
- All exposed C-FFI methods must use explicit unsafe raw pointer safety patterns, documenting lifecycle ownership transformations clearly (`Box::into_raw` and `Box::from_raw`).

```rust
// ✅ CORRECT: Documented FFI with clear ownership
/// # Safety
/// - `ptr` must be a valid pointer returned by `radix_tree_new()`
/// - Caller takes ownership of the returned pointer
#[no_mangle]
pub unsafe extern "C" fn radix_tree_lookup(
    ptr: *const RadixTree,
    ip: u128,
) -> *const GeoData {
    let tree = unsafe { &*ptr };
    match tree.lookup(Ipv6Addr::from(ip)) {
        Some(data) => data as *const GeoData,
        None => std::ptr::null(),
    }
}
```

### Python Layer (Testing & Data Generation)

```python
# ✅ GOOD: Generate structured test data
import random
import ipaddress
from typing import List, Tuple

def generate_mock_subnets(count: int) -> List[ipaddress.IPv6Network]:
    """Generate realistic IPv6 subnet distributions."""
    subnets = []
    for _ in range(count):
        # Simulate real-world ISP allocation patterns
        prefix = random.randint(32, 48)
        network = ipaddress.IPv6Network(
            f"2001:db8:{random.randint(0, 0xFFFF):x}::/{prefix}",
            strict=False
        )
        subnets.append(network)
    return subnets

def generate_lookups(subnets: List, count: int) -> List[str]:
    """Generate realistic lookup patterns with temporal locality."""
    lookups = []
    for _ in range(count):
        # 80% of lookups are repeated (simulates real traffic)
        if random.random() < 0.8 and lookups:
            ip = random.choice(lookups)
        else:
            subnet = random.choice(subnets)
            ip = str(random.choice(list(subnet.hosts())))
        lookups.append(ip)
    return lookups
```

---

## 🧪 Testing Requirements

### Go Tests

```go
func TestRadixTreeLookup(t *testing.T) {
    tree := NewRadixTree()
    
    // Test exact match
    ip := netip.MustParseAddr("2001:db8::1")
    data := tree.Lookup(ip)
    if data == nil {
        t.Errorf("Expected lookup to succeed for %v", ip)
    }
    
    // Test subnet match
    ip2 := netip.MustParseAddr("2001:db8:1234::2")
    data2 := tree.Lookup(ip2)
    if data2 == nil {
        t.Errorf("Expected subnet match for %v", ip2)
    }
}

func BenchmarkLookup(b *testing.B) {
    tree := NewRadixTree()
    ips := generateTestIPs(1000)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        tree.Lookup(ips[i%len(ips)])
    }
}
```

### Rust Tests

```rust
#[cfg(test)]
mod tests {
    use super::*;
    
    #[test]
    fn test_exact_match() {
        let tree = RadixTree::new();
        let ip = "2001:db8::1".parse().unwrap();
        assert!(tree.lookup(ip).is_some());
    }
    
    #[test]
    fn test_subnet_match() {
        let tree = RadixTree::new();
        let ip = "2001:db8:1234::2".parse().unwrap();
        assert!(tree.lookup(ip).is_some());
    }
}

#[bench]
fn bench_lookup(b: &mut Bencher) {
    let tree = RadixTree::new();
    let ips: Vec<Ipv6Addr> = generate_test_ips(1000);
    
    b.iter(|| {
        let idx = rand::random::<usize>() % ips.len();
        tree.lookup(ips[idx])
    });
}
```

---

## 🔒 Security & Memory Safety

### Go: No Unsafe

```go
// ✅ GOOD: Safe Go, no unsafe package
func (t *RadixTree) Insert(ip netip.Addr, data *GeoData) {
    // Safe, bounds-checked operations
    t.mu.Lock()
    defer t.mu.Unlock()
    t.root = t.root.insert(ip, data)
}
```

### Rust: Documented Unsafe

```rust
// ✅ GOOD: Unsafe encapsulated and documented
/// # Safety
/// - `ptr` must be non-null and properly aligned
/// - Caller guarantees the pointer is valid for the entire operation
#[no_mangle]
pub unsafe extern "C" fn radix_tree_insert(
    ptr: *mut RadixTree,
    ip: u128,
    data: *mut GeoData,
) -> bool {
    unsafe {
        let tree = &mut *ptr;
        let ip = Ipv6Addr::from(ip);
        let data = Box::from_raw(data);
        tree.insert(ip, *data)
    }
    // Ownership transferred to the tree
}
```

---

## 💬 Questions or Roadblocks?

If you hit architectural limitations, memory allocation traps, or pointer layout bugs while porting or optimizing, feel free to open a technical Issue or start a discussion thread. Let's build ultra-fast infrastructure together!