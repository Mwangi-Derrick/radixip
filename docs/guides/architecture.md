# Architecture

This document describes how RadixIP is structured as a deployed system, not
just as a data structure. For the data structure itself, see
[radix-tree-design.md](./radix-tree-design.md).

## The two-layer model

RadixIP separates *reading* from *distributing updates*:

```
                 ┌───────────────────────────────┐
                 │   API Gateway / Proxy Layer    │
                 └───────────────┬─────────────────┘
                                 │
                                 ▼
                 ┌───────────────────────────────┐
                 │   L1: In-process Radix Tree    │
                 │   (Go / Rust / FFI bindings)   │
                 │                                │
                 │   • No lock on reads           │
                 │   • Zero allocation on read    │
                 │   • Every Match() call stays   │
                 │     entirely local             │
                 └───────────────┬─────────────────┘
                                 │  (only consulted on
                                 │   insert/update/delete)
                                 ▼
                 ┌───────────────────────────────┐
                 │   L2: Redis                    │
                 │                                │
                 │   • Subnet → metadata records  │
                 │   • Pub/Sub for propagating     │
                 │     changes to other instances  │
                 │   • Optional persistence layer  │
                 └───────────────┬─────────────────┘
                                 │
                                 ▼
                 ┌───────────────────────────────┐
                 │   Durable storage (optional)   │
                 │   Postgres / MySQL / etc.      │
                 └───────────────────────────────┘
```

**The key property: Redis is never on the read/hot path.** Every `Match()`
call is answered entirely from the local, in-process radix tree. Redis is
only involved when a subnet is inserted, updated, or removed — at that
point, the change is written to Redis and published on a channel, and other
running instances subscribed to that channel pull the change into their own
local trees.

## Why not just query Redis directly?

You could skip the local tree and do a Redis lookup (or a Redis
CIDR-matching module) per request, but that reintroduces a network round
trip into your hot path — typically sub-millisecond for a local Redis
instance, but still orders of magnitude slower than an in-process lookup,
and it makes your service's latency dependent on Redis availability. The
two-layer design keeps the read path resilient to Redis being briefly
unavailable, since already-loaded instances keep serving from their local
tree.

## Update propagation flow

```
1. Operator/admin inserts or removes a subnet
                │
                ▼
2. Write to Redis (subnet → metadata)
                │
                ▼
3. Publish change on a Pub/Sub channel
                │
                ▼
4. Every subscribed instance receives the message
                │
                ▼
5. Each instance applies the change to its local tree
   via an atomic root-pointer swap (see radix-tree-design.md)
```

Propagation latency here is bounded by Redis Pub/Sub delivery time, not by
the cost of an individual lookup — this is why RadixIP's advertised lookup
latency figures (see [benchmark-methodology.md](./benchmark-methodology.md))
only describe the local `Match()` call, not the time to propagate an update.

## Concurrency model

- **Reads**: lock-free, using an atomically-swapped root pointer. Readers
  never block writers and vice versa.
- **Writes**: serialized per-instance (a single writer builds the updated
  tree structure and performs the atomic swap). Cross-instance consistency
  is eventually consistent, bounded by Pub/Sub delivery — not
  linearizable. If your use case requires strict consistency across
  instances (e.g. a security-critical block-list where a brief propagation
  delay is unacceptable), account for that window explicitly rather than
  assuming instant global consistency.

## Where this sits in a real deployment

RadixIP is a library, not a standalone service — it's meant to be embedded
directly into whatever component needs to make a fast, prefix-based
decision. Typical integration points:

- A **connection proxy** in front of a database (e.g. inside or alongside
  PgBouncer/ProxySQL), validating client IPs before the expensive TLS/auth
  handshake.
- An **API gateway or reverse proxy**, filtering or routing requests based
  on source subnet.
- A **service that needs geo or ASN metadata** for an IP, using RadixIP as
  a local cache in front of a slower external lookup, with Redis used to
  share cached entries across instances.

## Persistent storage

Redis itself can be configured for persistence (RDB/AOF), or you can back
it with a durable store (Postgres, MySQL, etc.) as the source of truth,
with Redis acting as a distribution/cache layer in front of it. RadixIP
doesn't prescribe a specific persistence strategy — it only defines the
local tree and the Pub/Sub-based update mechanism.

## Further reading

- [Radix Tree Design](./radix-tree-design.md) — the L1 data structure
- [Cache Locality](./cache-locality.md) — why the L1 layer is fast
- [Benchmark Methodology](./benchmark-methodology.md) — how L1 latency is measured