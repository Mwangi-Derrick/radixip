//! RadixIP XDP source-IP blocklist (kernel side).
//!
//! Map contract (shared with ebpf/xdp-c/xdp_blocklist.c and both loaders):
//!   blocklist_v4 : LPM trie, key = { prefixlen: u32, addr: [u8; 4] }, value u8 (1 = block)
//!   blocklist_v6 : LPM trie, key = { prefixlen: u32, addr: [u8; 16] }, value u8 (1 = block)
//!   stats        : per-CPU array of u64
//!                  0 v4 drop, 1 v4 pass, 2 v6 drop, 3 v6 pass, 4 parse error
//!
//! Address bytes are kept exactly as they appear on the wire (network order).
//! Never byte-swap them. Anything we cannot parse is passed (fail-open).
#![no_std]
#![no_main]

use aya_ebpf::{
    bindings::xdp_action,
    macros::{map, xdp},
    maps::{lpm_trie::Key, LpmTrie, PerCpuArray},
    programs::XdpContext,
};
use core::mem;

const ETH_HLEN: usize = 14;
const VLAN_HLEN: usize = 4;
const ETH_P_IP: u16 = 0x0800;
const ETH_P_IPV6: u16 = 0x86DD;
const ETH_P_8021Q: u16 = 0x8100;
const ETH_P_8021AD: u16 = 0x88A8;

const STAT_V4_DROP: u32 = 0;
const STAT_V4_PASS: u32 = 1;
const STAT_V6_DROP: u32 = 2;
const STAT_V6_PASS: u32 = 3;
const STAT_PARSE_ERR: u32 = 4;

// `pinned` => pinned by name under the loader's map_pin_path, so a warm
// restart reuses the same map. Changing key/value/max_entries requires
// bumping the pin directory version (see guide section 7.4).
#[map(name = "blocklist_v4")]
static BLOCKLIST_V4: LpmTrie<[u8; 4], u8> = LpmTrie::pinned(1 << 20, 0);

#[map(name = "blocklist_v6")]
static BLOCKLIST_V6: LpmTrie<[u8; 16], u8> = LpmTrie::pinned(1 << 20, 0);

#[map(name = "stats")]
static STATS: PerCpuArray<u64> = PerCpuArray::with_max_entries(5, 0);

#[inline(always)]
fn bump(idx: u32) {
    if let Some(p) = STATS.get_ptr_mut(idx) {
        unsafe { *p += 1 };
    }
}

/// Bounds-checked pointer into the packet. The check is what the verifier
/// needs to see before any dereference.
#[inline(always)]
unsafe fn ptr_at<T>(ctx: &XdpContext, offset: usize) -> Result<*const T, ()> {
    let start = ctx.data();
    let end = ctx.data_end();
    if start + offset + mem::size_of::<T>() > end {
        return Err(());
    }
    Ok((start + offset) as *const T)
}

#[xdp]
pub fn radixip_xdp(ctx: XdpContext) -> u32 {
    match try_xdp(&ctx) {
        Ok(action) => action,
        Err(_) => {
            bump(STAT_PARSE_ERR);
            xdp_action::XDP_PASS
        }
    }
}

#[inline(always)]
fn try_xdp(ctx: &XdpContext) -> Result<u32, ()> {
    // EtherType (big-endian on the wire) at offset 12.
    let et: [u8; 2] = unsafe { *ptr_at::<[u8; 2]>(ctx, 12)? };
    let mut proto = u16::from_be_bytes(et);
    let mut l3 = ETH_HLEN;

    // At most one VLAN tag (802.1Q or 802.1ad). Deeper stacks fall through
    // to the `_` arm below and are passed.
    if proto == ETH_P_8021Q || proto == ETH_P_8021AD {
        let inner: [u8; 2] = unsafe { *ptr_at::<[u8; 2]>(ctx, ETH_HLEN + 2)? };
        proto = u16::from_be_bytes(inner);
        l3 = ETH_HLEN + VLAN_HLEN;
    }

    match proto {
        ETH_P_IP => {
            // IPv4 source address is at byte 12 of the IP header.
            let src: [u8; 4] = unsafe { *ptr_at::<[u8; 4]>(ctx, l3 + 12)? };
            let key = Key::new(32, src);
            let hit = BLOCKLIST_V4.get(&key).map_or(false, |v| *v != 0);
            if hit {
                bump(STAT_V4_DROP);
                Ok(xdp_action::XDP_DROP)
            } else {
                bump(STAT_V4_PASS);
                Ok(xdp_action::XDP_PASS)
            }
        }
        ETH_P_IPV6 => {
            // IPv6 source address is at byte 8 of the fixed header.
            let src: [u8; 16] = unsafe { *ptr_at::<[u8; 16]>(ctx, l3 + 8)? };
            let key = Key::new(128, src);
            let hit = BLOCKLIST_V6.get(&key).map_or(false, |v| *v != 0);
            if hit {
                bump(STAT_V6_DROP);
                Ok(xdp_action::XDP_DROP)
            } else {
                bump(STAT_V6_PASS);
                Ok(xdp_action::XDP_PASS)
            }
        }
        _ => Ok(xdp_action::XDP_PASS),
    }
}

#[panic_handler]
fn panic(_: &core::panic::PanicInfo) -> ! {
    unsafe { core::hint::unreachable_unchecked() }
}