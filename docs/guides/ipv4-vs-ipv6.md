# IPv4 vs IPv6 in RadixIP

RadixIP performs longest prefix matching against both IPv4 and IPv6
addresses. The lookup algorithm is identical for both — the only real
difference is the number of bits being walked, and the conventions around
how those bits are typically subnetted. This document covers both, and is
also a general primer if you (like most people who haven't worked in
networking) aren't sure how IPv6 addressing actually works.

## IPv4: the 32-bit address space

```
192.168.1.42
```

```
Binary: 11000000.10101000.00000001.00101010
```

IPv4 addresses are 32 bits, giving roughly 4.3 billion possible addresses —
a number the internet has already exhausted, which is the whole reason IPv6
exists.

### Common IPv4 subnet sizes

| Prefix | Mask | Typical use |
|---|---|---|
| `/8` | 255.0.0.0 | Very large historical allocations (e.g. early corporate/military blocks) |
| `/16` | 255.255.0.0 | Large organizations |
| `/24` | 255.255.255.0 | Typical LAN / office network |
| `/32` | 255.255.255.255 | A single host |

IPv4 prefix lengths in the wild are **variable and somewhat arbitrary** —
shaped by decades of incremental allocation, not a clean hierarchy.

## IPv6: the 128-bit address space

```
2001:0db8:1234:abcd:0000:0000:0000:0001
```

IPv6 addresses are 128 bits — enough address space that exhaustion isn't a
practical concern. The address is written as eight groups of 16 bits in
hex, separated by colons, with consecutive zero groups collapsible to `::`
(so the address above can be written `2001:db8:1234:abcd::1`).

### Structure

A "normal" (globally routed, non-multicast) IPv6 address is conventionally
split like this:

```
┌────────────────────┬────────────────┬──────────────────────────┐
│   Global routing    │   Subnet ID    │      Interface ID        │
│   prefix (typ. /48) │  (typ. 16 bit) │      (typ. 64 bit)       │
└────────────────────┴────────────────┴──────────────────────────┘
```

### Common IPv6 subnet sizes

| Prefix | Typical use |
|---|---|
| `/32` | Large ISP allocation |
| `/48` | A single site or organization |
| `/56` | A smaller customer allocation (e.g. residential) |
| `/64` | A single LAN segment — the de facto standard subnet size (RFC 4291 recommends `/64` for any network with hosts) |
| `/127` | Point-to-point router links |
| `/128` | A single interface/host |

### The `/64` convention

Unlike IPv4, where subnet size is chosen case-by-case, IPv6 networking
practice treats `/64` as the standard building block for any subnet that
hosts devices. This is a strong, near-universal convention rather than a
hard protocol rule, but it holds in the overwhelming majority of real
deployments. It matters for RadixIP because it means IPv6 traffic tends to
arrive with more structure than IPv4 traffic: many devices behind the same
`/64` (a home network, a company floor, a cloud VPC subnet) will share
everything except their interface identifier.

## Same algorithm, different bit-width

This is the important part: **RadixIP's tree traversal doesn't know or care
whether it's walking an IPv4 or IPv6 address.** It's the same bit-by-bit
walk described in [longest-prefix-match.md](./longest-prefix-match.md) —
just bounded by 32 steps for IPv4 and 128 steps for IPv6.

```
IPv4: walk up to 32 bits  → root → bit31 → bit30 → ... → bit0
IPv6: walk up to 128 bits → root → bit127 → bit126 → ... → bit0
```

Internally, RadixIP stores IPv4 and IPv6 entries in separate trees (a 32-bit
tree and a 128-bit tree), since mixing bit-widths in one structure adds
complexity for no benefit — but the node design and traversal logic are
shared.

## What this means for caching strategy

Because IPv6 subnetting is more hierarchical and conventionally aligned to
`/64` boundaries, it's often possible to cache a *broader* prefix (e.g. a
`/64` or `/48`) and correctly cover many individual addresses under it,
whereas IPv4 traffic more often requires caching at a finer granularity
since prefix sizes in the wild vary more.

This is a structural observation, not a guaranteed performance multiplier —
actual cache hit rates depend entirely on the shape of your traffic and
what you choose to insert into the tree. If you want to know the caching
behavior for a *specific* workload, measure it against your own traffic
using the methodology in
[benchmark-methodology.md](./benchmark-methodology.md), rather than
assuming a general ratio.

## Notable IPv6-specific behavior

- **No NAT (usually)**: IPv6 was designed with enough address space that
  NAT generally isn't required. Devices typically get globally routable
  addresses, which simplifies (though doesn't guarantee) location
  attribution compared to IPv4 addresses that may sit behind carrier-grade
  NAT.
- **Privacy extensions (RFC 4941)**: many operating systems periodically
  rotate the interface identifier (the last 64 bits) of their IPv6 address
  for privacy. This doesn't affect RadixIP's `/64`-or-broader lookups, since
  the routing/subnet portion of the address is unchanged — only address-level
  (`/128`) caching would be affected by this rotation.
- **Dual stack**: RadixIP maintains independent IPv4 and IPv6 trees, so a
  service that sees both kinds of traffic can query each address type
  through the same API without special-casing.

## Further reading

- [Longest Prefix Match](./longest-prefix-match.md) — the shared algorithm
- [Radix Tree Design](./radix-tree-design.md) — the shared data structure
- [Benchmark Methodology](./benchmark-methodology.md) — how to measure real hit rates and latency for your own workload