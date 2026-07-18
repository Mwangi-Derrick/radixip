# RadixIP Architecture

RadixIP is a high-performance IP longest-prefix matching library with implementations in Rust and Go, bridged via FFI to Node.js, Python, C, and C++.

---

## Core Design Principle: Separation of Concerns

The architecture enforces a strict two-layer separation:

```
┌───────────────────────────────────────────────────────┐
│                   Engine Layer                        │
│  (Concurrency: RwLock, sharding, atomic ops, stats)   │
│  StandardEngine<T>  |  ShardedEngine<T>               │
└────────────────────────┬──────────────────────────────┘
                         │ owns via RouteTree trait
┌────────────────────────▼──────────────────────────────┐
│                    Tree Layer                          │
│  (Routing: insert, lookup, remove, prefix matching)   │
│  UncompressedTree   |  CompressedTree                 │
└───────────────────────────────────────────────────────┘
```

**The engine never touches routing logic. The tree never touches locks or stats.**  
This means adding a new tree type (e.g., Lulea, DXR) requires no changes to any engine.

---

## Tree Implementations

### UncompressedTree (Binary Trie)

A classic bit-by-bit binary trie. Each node stores a single bit decision (left = 0, right = 1).

```
Insert 192.168.1.0/24:
  Root → 1 → 1 → 0 → 0 → 0 → 0 → 0 → 0  ← [node with metadata]
         C1   C2  bits of 192.168.1...
```

**Complexity:**
- Insert: `O(prefix_length)` — no splitting required
- Lookup: `O(prefix_length)` — traverse bit by bit
- Memory: One node per bit → up to 32 nodes for /32 IPv4, 128 for /128 IPv6

**Best for:** Write-heavy workloads, BGP route churn, small tables, control-plane use.

---

### CompressedTree (Patricia / Radix Trie)

Each node stores a compressed bit-string (edge label) representing a sequence of bits, not just one. Non-branching chains of nodes are collapsed into a single node.

```
Insert 192.168.1.0/24 and 10.0.0.0/8:
  Root[edge=""]
   ├─ [edge="11000000 10101000 00000001"] → metadata(192.168.1.0/24)
   └─ [edge="00001010"]                  → metadata(10.0.0.0/8)
```

On a second insert that shares a prefix, the edge is **split** at the diverging bit:
```
Insert 192.168.2.0/24 (diverges at bit 23 from 192.168.1.0/24):
  [edge="11000000 10101000 0000000"] ← shared prefix (internal node)
   ├─ [edge="1"] → metadata(192.168.1.0/24)
   └─ [edge="0"] → metadata(192.168.2.0/24)  ← wait, reversed for demo clarity
```

**Complexity:**
- Insert: `O(k)` where k = branching points, but involves node splitting
- Lookup: `O(k)` — typically far fewer hops than prefix length
- Memory: One node per unique branch → 1–2 MB for 500K routes vs 4–8 MB uncompressed

**Best for:** Read-heavy workloads, large routing tables, data-plane (forwarding), packet lookup.

---

## Choosing Your Tree

| Scenario | Recommended Tree | Reason |
|---|---|---|
| BGP Route Server | `UncompressedTree` | High write throughput, minimal insert cost |
| Forwarding / FIB | `CompressedTree` | Shallow depth, cache-friendly reads |
| Both (split plane) | Both simultaneously | Control plane writes to uncompressed; FIB reads from compressed |
| Testing / Prototyping | `UncompressedTree` | Simpler to reason about |
| Memory constrained | `CompressedTree` | 4× lower memory for large tables |
| High BGP churn + fast reads | Both + Redis sync | See Hybrid Architecture below |

---

## Engine Implementations

| Engine | Concurrency Model | Best For |
|---|---|---|
| `StandardEngine<T>` | Single `RwLock` around tree ops | Low-to-medium concurrency |
| `ShardedEngine<T>` | 16+ independent shards, each with its own tree | High-throughput parallel lookups |
| `EngineWrapper` | Uniform dispatch, picks engine at runtime | FFI consumers, configuration-driven |

### Using Both Trees in the Same Application

Because engines are generic over `RouteTree`, you can run both trees simultaneously:

```rust
// Rust
let control_plane = StandardEngine::new(UncompressedTree::new(NodeVariant::Normal));
let data_plane    = StandardEngine::new(CompressedTree::new(NodeVariant::Normal));

// Fast insert on control plane
control_plane.insert(prefix, metadata.clone())?;

// Fast lookup on data plane (after sync)
let route = data_plane.lookup(&packet_ip);
```

```go
// Go
controlPlane := NewStandardEngine(NewUncompressedTree(NodeNormal))
dataPlane    := NewStandardEngine(NewCompressedTree(NodeNormal))
```

---

## Hybrid Architecture: Redis as the State Bus

For Cloudflare-scale deployments with both high write throughput and high read throughput:

```
BGP Updates ──► UncompressedTree (Control Plane)
                      │
                      │ HSET on insert/remove
                      ▼
                    Redis  ◄──── persistence / crash recovery
                      │
                      │ subscribe / periodic HGETALL
                      ▼
               CompressedTree (Data Plane / FIB)
                      │
                      ▼
              Packet Forwarding (millions/sec)
```

- **Control plane** handles writes with zero-cost inserts (no node splitting).
- **Redis** is the durable state bus — survives restarts.
- **Data plane** is rebuilt from Redis on startup (`HGETALL` boot-load) and receives async updates.
- **Recovery:** On crash, the data plane re-hydrates entirely from Redis in O(n).

---

## Performance Reference

> Numbers are approximate for a table of 500,000 IPv4 routes on modern hardware.

| Operation | UncompressedTree | CompressedTree |
|---|---|---|
| Insert | ~120 ns | ~300 ns (splitting) |
| Lookup | ~200 ns | ~80 ns (fewer hops) |
| Memory (500K routes) | ~4–8 MB | ~1–2 MB |
| Insert throughput (8 threads, ShardedEngine) | ~40M ops/s | ~18M ops/s |
| Lookup throughput (8 threads, ShardedEngine) | ~20M ops/s | ~55M ops/s |
| Lock-free potential | Easy (per-node CAS) | Hard (split requires multi-node CAS) |

---

## FFI Layer

Both tree types are exposed to FFI consumers (Node.js, Python, C, C++) through the `EngineWrapper` which accepts a `compressed: bool` flag at construction time. This avoids requiring FFI consumers to recompile the Rust binary.

```c
// C
RadixEngine* engine = radixip_new(ENGINE_STANDARD, NODE_NORMAL, /*compressed=*/false);
RadixEngine* fib    = radixip_new(ENGINE_CONCURRENT, NODE_ATOMIC, /*compressed=*/true);
```

```python
# Python
from radixip import Engine
control = Engine(variant="standard", compressed=False)
fib     = Engine(variant="concurrent", compressed=True)
```

---

## File Structure

```
lib/
├── rust/src/
│   ├── tree.rs        ← UncompressedTree + CompressedTree (RouteTree trait)
│   ├── engine.rs      ← StandardEngine<T> + ShardedEngine<T> + EngineWrapper
│   ├── traits.rs      ← RouteTree, RadixEngine, RadixNode traits
│   ├── node.rs        ← NormalNode, AtomicNode, PaddedNode + NodeBuilder
│   ├── lpm.rs         ← get_bit(), longest_prefix_match_binary()
│   ├── cache.rs       ← CachedEngine (in-memory LRU + Redis integration)
│   ├── redis.rs       ← Redis connection pool + HGET/HSET/HGETALL primitives
│   └── ffi.rs         ← C-compatible FFI exports
└── go/
    ├── tree.go        ← UncompressedTree + CompressedTree (RouteTree interface)
    ├── engine.go      ← StandardEngine, ShardedEngine, EngineWrapper
    ├── interfaces.go  ← RouteTree, RadixEngine, RadixNode interfaces
    ├── node.go        ← Node implementations + NodeBuilder
    ├── cache.go       ← CachedEngine
    └── redis.go       ← Redis integration
```
