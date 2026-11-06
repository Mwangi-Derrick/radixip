# RadixIP eBPF & XDP Packet Processing Architecture Guide

This guide describes how eBPF and XDP (eXpress Data Path) integrate with RadixIP for line-rate source-IP filtering, how the kernel data plane relates to the gRPC sidecars and the core ART engine, and the operational contract the loader must satisfy.

> **Status:** target architecture. The `ebpf/` directory and `lib/go/xdp/` are scaffolds, not production code. Sections marked **[planned]** describe intended behavior. Unmarked constraints come from the Linux kernel and the eBPF verifier.

### What changed in this revision

| # | Change | Why |
|---|---|---|
| 1 | LPM keys are `lpm_trie::Key<K>` (prefix length + data), never a bare address | The original sample could not match prefixes |
| 2 | Keys are byte arrays (`[u8; 4]`, `[u8; 16]`) in network order | Removes the `u32` / `from_be` endianness trap entirely |
| 3 | Separate IPv4 and IPv6 maps and a family branch | IPv6 was silently passed |
| 4 | Headers defined inline, no `network_types` dependency | Crate APIs differ between versions; inline structs are ~10 lines and verifier-friendly |
| 5 | `ptr_at` is a local helper, not a method on `XdpContext` | `XdpContext` has no `ptr_at`; the earlier sample would not compile |
| 6 | Deployment topologies, operational contract, metrics, pinning | Missing from the original |
| 7 | New pitfalls section (SNAT, self-lockout, bpffs in pods, mapped IPv6) | These are the failures that actually happen in production |
| 8 | Performance figures labeled as estimates; memory estimate corrected | The earlier figures were unsourced and the memory number was too low |

---

## 1. Edge (XDP) vs. Core Engine vs. gRPC (L7)

```text
 ┌─────────────────────────────────────────────────────────────────────────────┐
 │ INCOMING NETWORK PACKET (Host NIC / Wire)                                    │
 └──────────────────────────────────────┬──────────────────────────────────────┘
                                        │
                                        ▼
                  ┌───────────────────────────────────────────┐
                  │          eBPF / XDP Hook (L2 / L3)        │  tens–hundreds of ns
                  │       (NIC driver RX path, or generic)    │
                  └─────────────┬─────────────────────────────┘
                                │
             ┌──────────────────┴──────────────────┐
     (Match / Drop)                        (Pass)
             │                                     │
             ▼                                     ▼
     ┌──────────────┐              ┌───────────────────────────────┐
     │   XDP_DROP   │              │   Linux Kernel Network Stack  │
     │  (discarded) │              │   (TCP/IP, sk_buff, Sockets)  │
     └──────────────┘              └───────────────┬───────────────┘
                                                   │
                                                   ▼
                                   ┌───────────────────────────────┐
                                   │      RadixIP gRPC Sidecar     │  ~200 µs – 2 ms
                                   │     (L7: HTTP/2 / Protobuf)   │
                                   └───────────────────────────────┘
```

### Why XDP lives at the edge

- In **native (driver) mode**, XDP runs before `sk_buff` allocation. A dropped packet never costs a socket buffer or TCP state.
- In **generic (SKB) mode**, XDP runs after `sk_buff` allocation but still before the IP/TCP stack and sockets. You save the stack work but not the allocation.
- **LPM blocklists belong here.** `BPF_MAP_TYPE_LPM_TRIE` is a kernel-native longest-prefix-match structure.
- **gRPC belongs at L7.** By then the packet has been through TCP, TLS and HTTP/2 framing. XDP cannot live inside a gRPC handler. The gRPC sidecar is the *control plane* that writes policy into the XDP *data plane*.

### What XDP cannot do

XDP sees raw headers only: Ethernet, IP, TCP/UDP ports. It cannot see gRPC method names, HTTP headers, or TLS content.

| Layer | Sees | Decides |
|---|---|---|
| XDP + LPM_TRIE | Source IP (and ports if you extend it) | Drop blocked CIDRs, later L3/L4 rate limits |
| Middleware + ART | HTTP/gRPC headers, routing, auth | L7 policy, metadata (region, ISP, tenant), business logic |

---

## 2. Control plane + data plane

**Control plane (sidecar / core engine).** The ART engine stays the source of truth. A second subscriber on the existing `security:blocklist` channel mirrors each CIDR into the kernel LPM map. The kernel map holds only a 1-bit decision. Metadata stays in ART.

**Data plane (XDP program).** Per packet: parse Ethernet (+ one optional VLAN tag), branch on IPv4/IPv6, look up the source address, return `XDP_DROP` on a hit, otherwise `XDP_PASS`.

### Key encoding (critical)

The kernel key is `struct bpf_lpm_trie_key { __u32 prefixlen; __u8 data[]; }`. The prefix length is *part of the key*: `10.0.0.0/8` and `10.0.0.0/16` are distinct entries.

| Family | Aya key | Go / C key | Prefix range |
|---|---|---|---|
| IPv4 | `Key<[u8; 4]>` | `{ uint32 prefixlen; u8 addr[4] }` | 0–32 |
| IPv6 | `Key<[u8; 16]>` | `{ uint32 prefixlen; u8 addr[16] }` | 0–128 |

Rules:

- **Address bytes are in network order, always.** Use byte arrays so host endianness never enters the picture. If you use `u32`, the in-memory bytes must still be the wire bytes, and `from_be` on the lookup side will silently break matching on little-endian CPUs.
- **`prefixlen` is host-order `u32`.**
- **Lookups use `prefixlen = 32` / `128`.** The trie returns the longest stored prefix that covers the address.
- **Two maps are required.** Key sizes differ, and one `LPM_TRIE` has one key size.
- **`BPF_F_NO_PREALLOC` is mandatory** for LPM tries. Aya sets it for you. In C and Go-loaded objects you set it explicitly. Updates allocate kernel memory, so batch or rate-limit bulk changes.
- **Normalize before inserting.** `::ffff:198.51.100.0/120` must become `198.51.100.0/24` in the v4 map. Native packets never carry mapped addresses at L2, so a mapped entry in the v6 map will never match.
- **Map memory.** Budget tens of bytes per node. A 1M-entry map is in the tens of MB, not single digits. Measure with `bpftool map show` and cgroup accounting rather than trusting an estimate.

---

## 3. Rust vs. C

| | Rust (Aya) | C (libbpf / cilium-ebpf) |
|---|---|---|
| Fit | Rust sidecar, one toolchain | Go sidecar (`bpf2go`) |
| Toolchain | nightly + `rust-src` + `bpf-linker` | `clang`, `libbpf-dev`, kernel headers |
| Safety | Typed, but the kernel crate is still `unsafe`-heavy | Manual, verifier-checked |
| Dependencies in kernel code | Prefer none (inline headers) | None |

**Recommendation.** Rust sidecar uses Aya for kernel program and loader. Go sidecar uses a C program with `cilium/ebpf`. Both programs implement the same map contract (names, key layout, stats indices), so either loader can drive either object. One object with two loaders is a goal **[planned]**, not a given: the Aya object and the C object are separate builds that must stay in lockstep.

Pin crate versions (`aya`, `aya-ebpf`, `cilium/ebpf`) and re-check API names on upgrade. Both ecosystems move fast.

---

## 4. Repository layout

```text
radixip/
├── ebpf/
│   ├── Makefile
│   ├── xdp-kernel/              # Aya eBPF program (#![no_std])
│   │   ├── Cargo.toml
│   │   └── src/main.rs
│   ├── xdp-loader/              # Aya user-space loader
│   │   ├── Cargo.toml
│   │   └── src/main.rs
│   └── xdp-c/
│       └── xdp_blocklist.c      # C program for the Go loader
├── lib/go/xdp/
│   ├── gen.go                   # //go:generate bpf2go
│   ├── loader.go                # load, pin, seed, attach, put/delete, stats
│   ├── bridge.go                # Redis → map subscriber
│   └── xdp_test.go              # BPF_PROG_TEST_RUN tests
└── docs/guides/ebpf-xdp-integration-guide.md
```

---

## 5. The XDP program

Both implementations share this contract:

- Maps: `blocklist_v4`, `blocklist_v6` (LPM trie, value `u8`, `1` = block), `stats` (per-CPU array of `u64`).
- Stats indices: `0` v4 drop, `1` v4 pass, `2` v6 drop, `3` v6 pass, `4` parse error.
- Behavior: parse error, truncated packet, non-IP, or unknown VLAN depth → `XDP_PASS` (fail-open).

Core of the Rust version (full file: `ebpf/xdp-kernel/src/main.rs`):

```rust
#[map(name = "blocklist_v4")]
static BLOCKLIST_V4: LpmTrie<[u8; 4], u8> = LpmTrie::pinned(1 << 20, 0);

fn try_xdp(ctx: &XdpContext) -> Result<u32, ()> {
    // ... parse Ethernet + optional VLAN, producing `proto` and `l3` offset ...
    match proto {
        ETH_P_IP => {
            let src: *const [u8; 4] = unsafe { ptr_at(ctx, l3 + 12)? };
            let key = Key::new(32, unsafe { *src });          // bytes as on the wire
            let hit = BLOCKLIST_V4.get(&key).map_or(false, |v| *v != 0);
            // ...
        }
        ETH_P_IPV6 => { /* src at l3 + 8, Key::new(128, [u8; 16]) */ }
        _ => Ok(xdp_action::XDP_PASS),
    }
}
```

Why inline headers: the dependency-free version is a handful of lines, gives exact layout control, and avoids `network_types` API drift (field types changed across releases).

Limits of this program: it handles at most one VLAN tag, and it does not walk IPv6 extension headers (not needed, the source address is in the fixed header). It filters on source address only.

---

## 6. Deployment topologies

| Topology | Attach point | Mode | Drop cost (estimate) | Privileges |
|---|---|---|---|---|
| **Sidecar** | Pod `eth0` (veth, pod netns) | SKB (generic) | hundreds of ns – ~1 µs | `CAP_NET_ADMIN`, `CAP_BPF`, `CAP_PERFMON` in the pod |
| **DaemonSet** | Node NIC (`eth0`, `ens5`, …) | DRV (native) | tens of ns | privileged, or host-level `NET_ADMIN`+`BPF`+`PERFMON` |
| **Offload** | SmartNIC | HW | firmware-dependent | vendor-specific **[future]** |

The cost column is an order-of-magnitude estimate. **Benchmark on your own NIC, kernel and CNI** (Section 9) before quoting a number anywhere.

Notes:

- **veth and native mode.** Modern kernels support native XDP on veth, but with conditions (peer configuration, NAPI/GRO settings). SKB mode is the portable default for sidecars. Treat native on veth as an optimization to test, not an assumption.
- **Operator decides, not runtime detection.** The K8s operator picks the topology from the `RadixIPPolicy` annotation. Runtime detection is a fallback for bare Docker.
- **Sidecar is the default** because it works on every CNI and cloud. DaemonSet is for bare-metal / SR-IOV nodes with a native-capable NIC.
- **Source IP must be the real client IP at the attach point.** See Section 10.

```yaml
metadata:
  annotations:
    radixip.io/xdp-enabled: "true"
    radixip.io/xdp-mode: "sidecar"       # or "daemonset"
    radixip.io/xdp-interface: "eth0"
```

---

## 7. Operational contract

Every bullet is a load-bearing decision.

### 7.1 Opt-in only
Disabled by default. Enabled by `xdp.enabled: true` (config), `RADIXIP_XDP_ENABLED=true` (env), or the K8s annotation. Precedence: env > config > default. With nothing set, RadixIP behaves exactly as today.

### 7.2 Fail-open on attach failure
Missing capabilities, unsupported mode, verifier reject, map creation failure → log a warning and continue userspace-only. **XDP never prevents the server from starting.** In a standalone loader process, "continue" means park (do not exit), otherwise the orchestrator restart-loops the container.

| Tier | When | Filtering |
|---|---|---|
| 0 | XDP off or unavailable | ART + middleware |
| 1 | XDP attached, blocklist seeded | XDP LPM + ART |
| 2 | Token-bucket map live **[planned]** | + L3/L4 rate limit |
| 3 | AF_XDP redirect **[planned]** | XDP → UMEM → ART |

### 7.3 Fail-open on cold start
Order is fixed: **load (creates maps) → seed → attach.** If seeding fails, attach anyway with whatever the map holds (empty = pass all) and log the gap. `xdp.fail_closed: true` refuses to attach until seeded. It is documented as dangerous and off by default.

### 7.4 Map pinning
Pin under a versioned directory, e.g. `/sys/fs/bpf/radixip/v1/`:

- On restart the loader reuses pinned maps, so enforcement does not lapse across a warm restart.
- **Bump the directory version when key/value/max_entries change.** A pinned map with a different definition makes the load fail.
- **bpffs must be mounted into the container.** Pods usually do not have it. Mount a hostPath at `/sys/fs/bpf`.
- A reused map can be stale. Reconcile after reconnect by diffing the map against the Redis snapshot (generation key in a small control map **[planned]**). Insert-only replay will not remove entries that expired while the loader was down.
- If you also pin the XDP *link*, enforcement survives a loader crash, but you now own unpin/cleanup. Default is not to pin the link.

### 7.5 Metrics

| Metric | Type | Labels |
|---|---|---|
| `radixip_xdp_attached` | gauge | `topology` |
| `radixip_xdp_attach_errors_total` | counter | `reason` |
| `radixip_xdp_drops_total` | counter | `family` |
| `radixip_xdp_passes_total` | counter | `family` |
| `radixip_xdp_parse_errors_total` | counter | — |
| `radixip_xdp_map_entries` | gauge | `family` |
| `radixip_xdp_seed_duration_seconds` | histogram | — |
| `radixip_xdp_pinned_map_reused` | gauge | — |

A silent fail-open is the most dangerous state in the system. Metrics are how you detect it. Drops/passes come from the per-CPU `stats` map (sum across CPUs). Map entry counts are tracked in the loader (the kernel does not expose a cheap LPM count).

### 7.6 A/B benchmark gate
The E2E orchestrator runs with XDP off and on. README claims must cite both and keep three metrics distinct: ART lookup latency (userspace, unchanged by XDP), XDP drop cost, and RPS under attack. Do not present "ART is N ns" next to "XDP drops in N ns" as if they were the same operation.

### 7.7 Self-lockout guard
The control plane **rejects** prefixes shorter than a configured minimum (defaults: v4 `/8`, v6 `/16`) and prefixes overlapping an operator-supplied never-block list (cluster CIDR, node IPs, health-check sources, your own admin range). One bad Redis message must not be able to block `0.0.0.0/0`.

---

## 8. Control-plane flow

```text
Admin / auto-ban / operator
        │
        ▼
Redis  security:blocklist            (existing, unchanged)
        │
        ├──► ART engine .Insert(cidr, metadata)       [existing]
        │
        └──► XDP bridge: normalize → guard → Put()    [new]
                    │
                    ▼
             bpf_map_update_elem()  →  kernel LPM_TRIE  →  XDP program (per packet)
```

Deletions (including `AutoBanTracker` expiry) take the same path and call `bpf_map_delete_elem()`. The ART engine API does not change. The bridge is one more subscriber.

The message format and snapshot key in the scaffolds are **assumptions**. Adapt `parse_event` / `ParseEvent` and the snapshot key to whatever RadixIP already publishes.

---

## 9. Testing and verification

1. **Unit (no NIC):** `BPF_PROG_TEST_RUN`. The Go scaffold has `xdp_test.go` that feeds crafted v4/v6 frames through the loaded program and asserts `XDP_DROP` / `XDP_PASS`. It needs root (or `CAP_BPF`) and skips otherwise. This is where you assert the key-encoding and endianness behavior.
2. **Integration:** a veth pair in a network namespace. Attach in SKB mode, send traffic from the peer, and check the `stats` map and `bpftool map dump`.
3. **Inspect:** `bpftool prog show`, `bpftool map show`, `bpftool net` (shows attached XDP and mode).
4. **Benchmark:** use a packet generator on real hardware (`xdp-bench`/`xdp-trafficgen` from xdp-tools, or pktgen/TRex) for native-mode numbers. veth tests tell you about correctness, not NIC-level performance.
5. **Negative tests:** `/0` insert is rejected; mapped-IPv6 entry lands in the v4 map; truncated frame passes; VLAN-tagged frame is filtered.

---

## 10. Pitfalls

- **SNAT / load balancers.** If a cloud LB, kube-proxy SNAT, or a service mesh rewrites the source, XDP sees the node or proxy IP, and blocklisting it is catastrophic. Use `externalTrafficPolicy: Local` or an LB that preserves client IPs (PROXY protocol does not help at XDP, which cannot parse it). Verify the source IP at the attach point before enabling.
- **Capabilities and kernel versions.** `CAP_BPF`/`CAP_PERFMON` need Linux ≥ 5.8 (older kernels need `CAP_SYS_ADMIN`). BPF-link-based XDP attach needs ≥ 5.7. LPM trie needs ≥ 4.11. Test on your minimum supported kernel.
- **Managed K8s.** Some platforms restrict privileged pods or host mounts. Daemonset mode may not be available.
- **Another XDP program already on the NIC.** Many CNIs (Cilium) attach their own. Attaching a second program can fail or replace it. Coordinate (xdp-dispatcher / libxdp multi-prog) before using daemonset mode on a CNI that uses XDP.
- **Generic mode and offloads.** Generic XDP interacts with GRO/LRO and some drivers. Test with your NIC offload settings.
- **IPv4-mapped IPv6** entries must be normalized to v4 (Section 2).
- **Verifier budget.** Keep the program small. Prefer `#[inline(always)]` and bounds-check every pointer before use.

---

## 11. Build order

### Local verification

From a fresh Linux clone, run the quick tests first:

```bash
make -C ebpf verify-unit
```

This checks config deserialization and loader-side IP normalization/guards. It
does not require a BPF object, root, or kernel capabilities. To reproduce the
full XDP CI job, run:

```bash
make -C ebpf verify
```

The full target installs nightly Rust, `rust-src`, and `bpf-linker` if needed,
builds the kernel object into the ignored repository `target/` directory, then
checks the embedded loader and gRPC sidecar and runs the unit tests. It needs a
working network connection for first-time toolchain/dependency installation.
Attaching XDP to an interface and measuring packet behavior still requires
Linux and the relevant BPF/network capabilities; these build checks do not
claim to be a performance benchmark.

The GitHub Actions workflow runs the same `make -C ebpf verify` command. The
generated object is intentionally not committed; Cargo embeds it from the local
build output.

| Step | Artifact | Why first |
|---|---|---|
| 1 | `xdp-kernel` (Rust) and `xdp_blocklist.c`, with the Go `BPF_PROG_TEST_RUN` tests | Validates key encoding, byte order, v4/v6 branch |
| 2 | Loader: capability check, pin, seed-before-attach, fail-open, stats | Encodes the contract in Section 7 |
| 3 | Redis → map bridge with guard and normalization | The actual policy sync |
| 4 | Metrics exporter wired to your existing endpoint | Lets operators see Tier 0 vs Tier 1 |
| 5 | K8s operator: `xdp.enabled`, `xdp.mode`, capability and bpffs injection | Makes it deployable |
| 6 | E2E phase: blocked IP reaches 0 requests at app layer | Acceptance test |
| 7 | README + A/B benchmark table | Justifies the complexity |

Do 1 and 2 first. If seed-before-attach is not clean in the loader, revise Section 7 before building on it. Token bucket (Tier 2) and AF_XDP (Tier 3) are additive and deferred.