// SPDX-License-Identifier: GPL-2.0
// RadixIP XDP source-IP blocklist (C version, driven by lib/go/xdp via cilium/ebpf).
//
// Must stay in lockstep with ebpf/xdp-kernel/src/main.rs:
//   blocklist_v4 / blocklist_v6 : LPM tries, value u8 (1 = block)
//   stats : per-CPU array, 0 v4 drop, 1 v4 pass, 2 v6 drop, 3 v6 pass, 4 parse error
// Addresses are compared as wire bytes; never byte-swap them.

//go:build ignore

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/in.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#define ETH_P_8021AD_ 0x88A8

struct lpm_v4_key {
	__u32 prefixlen;
	__u8 addr[4];
};

struct lpm_v6_key {
	__u32 prefixlen;
	__u8 addr[16];
};

struct {
	__uint(type, BPF_MAP_TYPE_LPM_TRIE);
	__type(key, struct lpm_v4_key);
	__type(value, __u8);
	__uint(max_entries, 1 << 20);
	__uint(map_flags, BPF_F_NO_PREALLOC); // mandatory for LPM tries
} blocklist_v4 SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LPM_TRIE);
	__type(key, struct lpm_v6_key);
	__type(value, __u8);
	__uint(max_entries, 1 << 20);
	__uint(map_flags, BPF_F_NO_PREALLOC);
} blocklist_v6 SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__type(key, __u32);
	__type(value, __u64);
	__uint(max_entries, 5);
} stats SEC(".maps");

enum { ST_V4_DROP, ST_V4_PASS, ST_V6_DROP, ST_V6_PASS, ST_PARSE_ERR };

static __always_inline void bump(__u32 idx)
{
	__u64 *v = bpf_map_lookup_elem(&stats, &idx);
	if (v)
		*v += 1;
}

SEC("xdp")
int radixip_xdp(struct xdp_md *ctx)
{
	void *data = (void *)(long)ctx->data;
	void *end = (void *)(long)ctx->data_end;

	struct ethhdr *eth = data;
	if ((void *)(eth + 1) > end) {
		bump(ST_PARSE_ERR);
		return XDP_PASS;
	}

	__u16 proto = eth->h_proto; // network order
	__u32 l3 = sizeof(*eth);

	// At most one VLAN tag.
	if (proto == bpf_htons(ETH_P_8021Q) || proto == bpf_htons(ETH_P_8021AD_)) {
		__be16 *inner = data + sizeof(*eth) + 2;
		if ((void *)(inner + 1) > end) {
			bump(ST_PARSE_ERR);
			return XDP_PASS;
		}
		proto = *inner;
		l3 = sizeof(*eth) + 4;
	}

	if (proto == bpf_htons(ETH_P_IP)) {
		__u8 *src = data + l3 + 12;
		if ((void *)(src + 4) > end) {
			bump(ST_PARSE_ERR);
			return XDP_PASS;
		}
		struct lpm_v4_key key = { .prefixlen = 32 };
		__builtin_memcpy(key.addr, src, 4);
		__u8 *hit = bpf_map_lookup_elem(&blocklist_v4, &key);
		if (hit && *hit) {
			bump(ST_V4_DROP);
			return XDP_DROP;
		}
		bump(ST_V4_PASS);
		return XDP_PASS;
	}

	if (proto == bpf_htons(ETH_P_IPV6)) {
		__u8 *src = data + l3 + 8;
		if ((void *)(src + 16) > end) {
			bump(ST_PARSE_ERR);
			return XDP_PASS;
		}
		struct lpm_v6_key key = { .prefixlen = 128 };
		__builtin_memcpy(key.addr, src, 16);
		__u8 *hit = bpf_map_lookup_elem(&blocklist_v6, &key);
		if (hit && *hit) {
			bump(ST_V6_DROP);
			return XDP_DROP;
		}
		bump(ST_V6_PASS);
		return XDP_PASS;
	}

	return XDP_PASS;
}

char _license[] SEC("license") = "GPL";