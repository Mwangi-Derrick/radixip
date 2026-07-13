# How Routers Work

RadixIP doesn't invent a new algorithm. It takes the algorithm every IP router
on the internet already runs, and applies it inside application code. This
document explains that original context, so the rest of the docs make sense.

## The path a packet takes

When your laptop talks to a server, the packet passes through several hops,
and at almost every hop, something has to make a routing decision:

```
Your device
    │
    ▼
Home / office router
    │
    ▼
ISP edge router
    │
    ▼
Backbone router(s)
    │
    ▼
Destination network's edge router
    │
    ▼
Firewall / load balancer
    │
    ▼
Destination server
```

At each of those routing points, the device holds a **routing table** — a
list of network prefixes and where to send traffic for each one. A backbone
router might hold 900,000+ entries. It cannot do a linear scan through all of
them for every packet, and a plain hashmap can't help either, because routing
isn't about exact matches — it's about finding the *most specific* prefix
that contains the destination address. That's the problem longest prefix
match (LPM) solves, and it's covered in detail in
[longest-prefix-match.md](./longest-prefix-match.md).

## Why routers use radix/Patricia trees

Router firmware and software routing stacks (Linux's FIB, BGP daemons like
BIRD and FRR, etc.) commonly implement their forwarding tables using radix or
Patricia tries for exactly the reasons covered in
[radix-tree-design.md](./radix-tree-design.md): the lookup key is a fixed-width
bit string (32 or 128 bits), traversal is a simple walk down the bits, and the
structure naturally produces the longest matching prefix along the way.

## Where RadixIP fits

RadixIP takes that same technique and moves it up the stack, into your
application or proxy layer, for decisions that are structurally identical to
routing even though they aren't about forwarding packets:

```
Client IP arrives
    │
    ▼
Is this IP in an allowed subnet?        ← ACL / whitelist
Which region does this subnet belong to? ← geo-routing
Is this subnet on a block list?          ← abuse mitigation
```

All three of those are "find the longest matching prefix for this address"
problems — the same shape of question a router answers, just with different
metadata attached to each prefix (a country code instead of a next-hop
interface, for example).

## What this does and doesn't mean

It's worth being precise here: RadixIP is not a router, doesn't do packet
forwarding, and doesn't replace your firewall or BGP stack. It's a
library for making prefix-based decisions *inside application code* — for
example, in a connection proxy sitting in front of a database, or in a
gateway deciding how to route a request. The value is borrowing a
well-understood, decades-old technique rather than reaching for a hashmap
that isn't suited to prefix matching, or a naive linear scan over CIDR
ranges that doesn't scale.

## Further reading

- [Longest Prefix Match](./longest-prefix-match.md) — the algorithm itself
- [Radix Tree Design](./radix-tree-design.md) — the data structure
- [Architecture](./architecture.md) — how this fits into a real deployment