# RadixIP eBPF & XDP Packet Processing Architecture Guide

This guide outlines how to integrate **eBPF (Extended Berkeley Packet Filter)** and **XDP (eXpress Data Path)** with RadixIP for line-rate packet filtering, and how it interacts with the gRPC sidecars and core engine.

---

## 1. Edge (XDP) vs. Core Engine vs. gRPC (L7)

Understanding the network layer separation is essential:

```text
 ┌─────────────────────────────────────────────────────────────────────────────┐
 │ INCOMING NETWORK PACKET (Host NIC / Wire)                                    │
 └──────────────────────────────────────┬──────────────────────────────────────┘
                                        │
                                        ▼
                  ┌───────────────────────────────────────────┐
                  │          eBPF / XDP Hook (L2 / L3)        │  < 100 nanoseconds
                  │       (Directly in NIC Driver RX ring)    │
                  └─────────────┬─────────────────────────────┘
                                │
             ┌──────────────────┴──────────────────┐
     (Match / Drop)                        (Pass)
             │                                     │
             ▼                                     ▼
     ┌──────────────┐              ┌───────────────────────────────┐
     │   XDP_DROP   │              │   Linux Kernel Network Stack  │
     │  (Instantly  │              │   (TCP/IP, sk_buff, Sockets)  │
     │   discarded) │              └───────────────┬───────────────┘
     └──────────────┘                              │
                                                   ▼
                                   ┌───────────────────────────────┐
                                   │      RadixIP gRPC Sidecar     │  ~ 200µs - 2ms
                                   │     (L7: HTTP/2 / Protobuf)   │
                                   └───────────────────────────────┘
```

### Why XDP Must Live at the Edge (Node / NIC Ingress)
- **XDP operates before `sk_buff` allocation**: It executes directly in the network driver before Linux allocates memory for the packet or initiates TCP state machines.
- **DDoS and LPM blocklists belong at XDP**: A malicious IP or blocked subnet can be rejected in **nanoseconds** without consuming any kernel TCP stack CPU or socket buffers.
- **gRPC belongs at Layer 7**: By the time a packet reaches gRPC, it has undergone TCP handshake, TLS decryption, HTTP/2 frame parsing, and Protobuf deserialization. XDP cannot be "inside" a gRPC handler—instead, **gRPC controls the eBPF data plane**.

---

## 2. Recommended Pattern: Control Plane (gRPC) + Data Plane (eBPF LPM Trie)

The Linux kernel has a built-in map type designed specifically for longest prefix matching: **`BPF_MAP_TYPE_LPM_TRIE`**.

1. **Control Plane (gRPC Sidecar / Core Engine)**:
   - Exposes `Insert(prefix, metadata)`, `Remove(prefix)`, etc.
   - When a rule is added (e.g. block `198.51.100.0/24`), the user-space daemon writes the CIDR and rule into the kernel's `BPF_MAP_TYPE_LPM_TRIE`.
2. **Data Plane (XDP Hook)**:
   - As packets arrive on the network interface, the XDP program extracts the IPv4/IPv6 source address.
   - Queries `bpf_map_lookup_elem(&lpm_trie, &src_ip)`.
   - Returns `XDP_DROP` if marked for blocking or rate exceeded.
   - Returns `XDP_PASS` to allow legitimate traffic up to the application / gRPC sidecar.

---

## 3. Rust vs. C: Which One to Choose?

| Metric | Rust (`Aya`) | C (`libbpf` / CO-RE) |
|---|---|---|
| **Ecosystem Fit** | **Ideal for RadixIP** (matches Rust engine and Rust gRPC server) | Great if writing user-space daemons in Go (`cilium/ebpf`) |
| **Toolchain** | Pure Rust (`cargo xtask` or standard cargo) | Requires `clang`, `llvm`, `bpftool`, and `vmlinux.h` |
| **Safety** | Memory safety and strong typing across kernel and user space | Manual pointer arithmetic, standard C verifier rules |
| **Learning Curve** | Modern, idiomatic `#![no_std]` Rust | Traditional Linux kernel C style |

### Recommendation
- If building the loader/controller with **Rust**: Use **[Aya](https://aya-rs.dev/)**. It allows writing both the eBPF kernel program (`#![no_std]`) and user-space loader in 100% Rust.
- If building the user-space loader with **Go**: Write the eBPF C program and use **[cilium/ebpf](https://github.com/cilium/ebpf)** with `bpf2go`.

---

## 4. Proposed Repository Layout

Add an `ebpf/` directory at the project root:

```text
radixip/
├── ebpf/
│   ├── xdp-kernel/            # Rust eBPF program running in kernel (#![no_std])
│   │   ├── Cargo.toml
│   │   └── src/
│   │       └── main.rs        # XDP hook: inspects IP header & queries LPM map
│   ├── xdp-loader/            # User-space Rust daemon (Aya)
│   │   ├── Cargo.toml
│   │   └── src/
│   │       └── main.rs        # Attaches XDP to eth0, opens sync with gRPC / unix socket
│   └── xtask/                 # Cargo xtask to build eBPF bytecode
```

---

## 5. Sample Aya (Rust) XDP Program

```rust
// ebpf/xdp-kernel/src/main.rs
#![no_std]
#![no_main]

use aya_ebpf::{
    bindings::xdp_action,
    macros::{map, xdp},
    maps::LpmTrie,
    programs::XdpContext,
};
use aya_log_ebpf::info;
use core::mem;
use network_types::{
    eth::{EthHdr, EtherType},
    ip::Ipv4Hdr,
};

#[map]
static BLOCKLIST_LPM: LpmTrie<u32, u8> = LpmTrie::with_max_entries(65536, 0);

#[xdp]
pub fn radixip_xdp(ctx: XdpContext) -> u32 {
    match try_radixip_xdp(ctx) {
        Ok(ret) => ret,
        Err(_) => xdp_action::XDP_PASS,
    }
}

fn try_radixip_xdp(ctx: XdpContext) -> Result<u32, ()> {
    let ethhdr: *const EthHdr = unsafe { ctx.ptr_at(0)? };
    if unsafe { (*ethhdr).ether_type } != EtherType::Ipv4 {
        return Ok(xdp_action::XDP_PASS);
    }

    let ipv4hdr: *const Ipv4Hdr = unsafe { ctx.ptr_at(EthHdr::LEN)? };
    let src_ip = u32::from_be(unsafe { (*ipv4hdr).src_addr });

    // LPM lookup in eBPF map
    if let Some(&action) = BLOCKLIST_LPM.get(&src_ip) {
        if action == 1 { // 1 = DROP
            return Ok(xdp_action::XDP_DROP);
        }
    }

    Ok(xdp_action::XDP_PASS)
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    unsafe { core::hint::unreachable_unchecked() }
}
```
