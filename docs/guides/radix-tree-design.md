# Radix Tree Design

This document explains why RadixIP is built on a binary radix (Patricia)
tree rather than a hashmap or a standard trie, and how the node layout is
structured.

## The three candidates

| Structure | Exact match | Longest prefix match | Memory | Notes |
|---|---|---|---|---|
| Hash map | Excellent, O(1) | Not supported | Medium | Great for exact keys, no concept of "contains" |
| Standard trie | Yes | Yes | High | One node per bit level, lots of single-child chains |
| Binary radix / Patricia tree | Yes | Yes | Low | Path-compressed, well suited to fixed-width bit keys like IPs |

### Why not a hashmap

A hashmap can answer "is this exact IP a key?" but has no way to answer
"which stored *subnet* contains this IP?" without scanning every entry. LPM
requires structure the hashmap doesn't have. This is explained in more
detail in [longest-prefix-match.md](./longest-prefix-match.md).

### Why not a standard (uncompressed) trie

A standard binary trie for IPv4 has, in the worst case, 32 levels of nodes
for every distinct prefix path, most of which have only one child. That's a
lot of pointer chasing and wasted memory for information that doesn't
actually branch. A radix (Patricia) tree compresses runs of single-child
nodes into a single edge, cutting both the memory footprint and the number
of pointer hops needed per lookup.

## Bit-level traversal

Every IPv4 address is 32 bits. Instead of hashing it, RadixIP walks those
bits directly:

```
IP: 192.168.1.42

Binary: 11000000.10101000.00000001.00101010
         │
         ├── bit 31 = 1 → go right
         ├── bit 30 = 1 → go right
         ├── bit 29 = 0 → go left
         └── ... continue for up to 32 bits
```

This traversal is deterministic and requires no extra indexing structure —
the tree shape *is* the index.

## Node layout

A pointer-chasing tree (`left *Node`, `right *Node`, heap-allocated per
node) works, but each pointer dereference is a potential cache miss (see
[cache-locality.md](./cache-locality.md)). RadixIP favors a flat,
array-backed layout where possible:

```
type Tree struct {
    nodes []Node   // contiguous slice, indices act as pointers
}

type Node struct {
    children [2]uint32   // indices into nodes[], 0 = no child
    metadata *Metadata   // nil if this isn't a terminal node
}
```

Storing nodes contiguously means that as the traversal proceeds, nearby
nodes are more likely to already be in the same cache line or have been
brought in by the CPU's hardware prefetcher — a meaningful win at the
lookup volumes this library targets.

## Insert and delete

- **Insert**: walk (or create) nodes along the bit path of the given prefix,
  down to the depth implied by its mask length, and attach metadata at that
  node.
- **Delete**: walk to the node for the given prefix and clear its metadata.
  Nodes that become "dead weight" (no children, no metadata) can optionally
  be pruned, though RadixIP's read path doesn't require this for
  correctness.

Both operations are O(prefix length) — bounded by 32 for IPv4, 128 for
IPv6.

## Concurrency

Reads (`Match`) don't take locks. Updates use an atomic pointer swap of the
root (`sync/atomic.Pointer` in Go, an equivalent atomic swap in Rust): a
writer builds a new (or partially copied) tree structure and swaps the root
pointer in one atomic operation. In-flight readers keep using the old tree
until they naturally re-read the root pointer, so writers never block
readers.

## Further reading

- [Longest Prefix Match](./longest-prefix-match.md) — the algorithm this structure implements
- [Cache Locality](./cache-locality.md) — why the memory layout choices above matter in practice
- [IPv4 vs IPv6](./ipv4-vs-ipv6.md) — how the same design extends to 128-bit keys