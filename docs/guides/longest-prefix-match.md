# Longest Prefix Match (LPM)

Longest prefix match is the core algorithm RadixIP implements. It's also the
algorithm every IP router uses to decide where a packet goes next. This
document explains it from scratch.

## The problem

You have a set of network prefixes, each with some associated data (a
next-hop, a region, a policy — RadixIP calls this "metadata"). Given a single
IP address, you need to find the **most specific** prefix that contains it.

"Most specific" means the prefix with the longest mask — a `/24` is more
specific than a `/16`, which is more specific than a `/8`.

### Example

Suppose your table (a set of inserted CIDR blocks) contains:

```
192.168.0.0/16
192.168.1.0/24
192.168.1.32/27
192.168.1.48/28
```

And you look up `192.168.1.50`. Checking which prefixes contain it:

```
192.168.0.0/16   → contains 192.168.1.50  (covers 192.168.0.0–192.168.255.255)
192.168.1.0/24   → contains 192.168.1.50  (covers 192.168.1.0–192.168.1.255)
192.168.1.32/27  → contains 192.168.1.50  (covers 192.168.1.32–192.168.1.63)
192.168.1.48/28  → contains 192.168.1.50  (covers 192.168.1.48–192.168.1.63)
```

All four match. The answer is the longest one: `192.168.1.48/28`.

## Why exact-match structures can't do this

A hashmap answers "does this exact key exist?" in O(1). It has no concept of
"a broader entry that also covers this key." You could check every prefix
against the address individually, but that's O(number of prefixes) per
lookup — unworkable once you have thousands of entries and need
sub-microsecond responses.

## The algorithm

A binary trie (radix tree) solves this cleanly:

1. Start at the root.
2. Walk the address one bit at a time, moving left on `0`, right on `1`.
3. At each node visited along the way, check whether it's a "terminal" node
   (i.e., some inserted prefix ends exactly there).
4. Keep track of the *last* terminal node seen — since you're walking from
   least-specific toward most-specific, the last terminal you pass is
   automatically the longest match.
5. Stop when you run out of bits or the tree has no further child in that
   direction.
6. Return the last terminal seen (or "no match" if none was found).

### Visualized

```
root
 │
 ▼ (bit 1)      ← 192.168.0.0/16 terminal here
 │
 ▼ (bit 1)      ← 192.168.1.0/24 terminal here
 │
 ▼ (bit 0)      ← 192.168.1.32/27 terminal here
 │
 ▼ (bit 0)      ← 192.168.1.48/28 terminal here  ✅ longest match, keep this one
 │
 ... (no further terminal deeper)
```

Because you only ever move forward and remember the deepest terminal, the
whole lookup happens in a single pass — no backtracking required.

## Complexity

| Operation | Complexity |
|---|---|
| Insert | O(prefix length) |
| Lookup | O(address bit-width) |
| Memory | O(number of inserted prefixes) |

For IPv4, address bit-width is 32, so a lookup never does more than 32 steps
regardless of how many prefixes are stored. For IPv6, it's 128 steps. Both
are small, fixed upper bounds — which is what makes lookup latency so
predictable (see [benchmark-methodology.md](./benchmark-methodology.md) for
measured numbers).

## Where LPM shows up outside networking

The same shape of problem appears anywhere you need "find the most specific
rule that applies":

- **Firewall/ACL rules** — narrower subnet rules should override broader ones.
- **Geo-routing** — a `/24` mapped to a specific city should win over a `/8`
  mapped to a whole country.
- **Abuse/rate-limit lists** — blocking a `/24` you've identified as
  malicious shouldn't be overridden by a broader `/16` allow rule if the
  `/24` is more specific.

RadixIP's `Match()` (or `match_ip()` in Rust) implements exactly this
algorithm, walking either 32 bits (IPv4) or 128 bits (IPv6) per lookup — see
[ipv4-vs-ipv6.md](./ipv4-vs-ipv6.md) for how that difference is handled.

## Further reading

- [Radix Tree Design](./radix-tree-design.md) — the data structure that makes this efficient
- [Cache Locality](./cache-locality.md) — why the *implementation* of this walk matters as much as the algorithm