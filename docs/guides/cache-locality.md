# Cache Locality

RadixIP's performance target isn't just "fewer operations" — it's "fewer
*cache misses*." This document explains why that distinction matters and how
it shapes the implementation.

## Why memory access dominates

On a modern CPU, the cost of an instruction is often dwarfed by the cost of
waiting for the data it operates on. Approximate latencies (these vary by
hardware, but the relative gap is the point):

| Location | Typical latency |
|---|---|
| CPU register | < 1 ns |
| L1 cache | ~1 ns |
| L2 cache | ~3–4 ns |
| L3 cache | ~10–20 ns |
| Main memory (DRAM) | ~60–100 ns |

A single main-memory access can cost 100x what an L1 hit costs. An algorithm
that does fewer "steps" but scatters those steps across unpredictable memory
locations can easily lose to one that does more steps but stays in cache.

## Spatial locality

**Spatial locality** is the principle that if a program accesses memory
address X, it's likely to access memory near X soon after. CPUs exploit this
with hardware prefetchers that watch access patterns and speculatively load
nearby cache lines before they're explicitly requested.

### Sequential access (cache-friendly)

```go
for i := 0; i < len(arr); i++ {
    sum += arr[i]
}
```

The prefetcher recognizes the stride and starts pulling in `arr[i+1]`,
`arr[i+2]`, etc. before the loop asks for them.

### Random access (cache-hostile)

```go
for i := 0; i < len(arr); i++ {
    sum += arr[rand.Intn(len(arr))]
}
```

No pattern to prefetch. Each access is a coin flip on whether it's already
cached.

## Why a hashmap struggles here

A typical hashmap lookup for a string key involves: hashing the key
(computation, but also memory reads over the key bytes), computing a bucket
index (effectively a pseudo-random jump), and then following a
pointer/chain into that bucket. None of these steps have a predictable
relationship to the *previous* lookup's memory location — each lookup tends
to land somewhere unrelated in memory, which is exactly the pattern hardware
prefetchers can't help with.

This isn't a criticism of hashmaps in general — they're excellent for exact
key lookups, and O(1) average case is genuinely fast for many workloads. It
just means the *access pattern*, not just the big-O complexity, matters for
IP-lookup-shaped problems at high volume.

## Why RadixIP's traversal is more cache-friendly

The bit-walk described in
[longest-prefix-match.md](./longest-prefix-match.md) moves from parent node
to child node at each step. If those nodes are stored so that children tend
to be near their parents in memory (see the flat-array layout in
[radix-tree-design.md](./radix-tree-design.md)), the traversal has much more
locality than a hashmap's bucket-and-chain pattern — each step is a small,
predictable jump rather than an arbitrary one.

### Pointer-chasing vs flat arrays

```go
// Pointer-based: each node individually heap-allocated
type Node struct {
    left  *Node
    right *Node
}
// left/right point to arbitrary heap addresses — no locality guarantee

// Flat array: nodes stored contiguously, children referenced by index
type Tree struct {
    nodes []Node
}
type Node struct {
    children [2]uint32 // indices into nodes[]
}
// Traversal moves through a single contiguous allocation
```

The flat-array version doesn't guarantee that a node's children sit next to
it in the array (that depends on insertion order), but it does guarantee
there's no independent heap allocation per node, avoids the allocator's own
overhead and fragmentation, and keeps the whole structure in far fewer
distinct memory pages than a pointer-heavy tree of the same size — which in
turn improves the odds of staying resident in L2/L3 cache.

## Zero allocations on the read path

RadixIP's `Match`/`match_ip` calls avoid heap allocation entirely. This
matters for two independent reasons:

1. **Latency predictability** — no allocation means no chance of the read
   path triggering garbage collection work (relevant in Go) or an allocator
   lock (relevant in any language), which keeps tail latency stable.
2. **No new cache pressure** — allocating new memory means touching cache
   lines that weren't already hot, which can evict data you were relying on
   staying cached.

## The takeaway

Algorithmic complexity (`O(1)` vs `O(32)`, etc.) is only part of the
performance story. On modern hardware, a structure that does a small,
bounded number of steps *with good memory locality* frequently outperforms
a structure with better asymptotic complexity but poor locality — which is
the practical justification for choosing a radix tree over a hashmap for
this specific problem. The measured numbers behind this claim, and the
methodology used to produce them, are in
[benchmark-methodology.md](./benchmark-methodology.md).

## Further reading

- [Radix Tree Design](./radix-tree-design.md) — the node layout referenced above
- [Benchmark Methodology](./benchmark-methodology.md) — how the latency numbers are actually measured