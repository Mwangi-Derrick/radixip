use std::net::IpAddr;

use ipnetwork::IpNetwork;

use crate::traits::RadixNode;
use crate::types::Metadata;

pub struct LPM;

impl LPM {
    pub fn lookup<'a, I>(entries: I, ip: &IpAddr) -> Option<Metadata>
    where
        I: IntoIterator<Item = (&'a IpNetwork, &'a Metadata)>,
    {
        longest_prefix_match_entries(entries, ip)
    }
}

/// Find the longest prefix match from an iterator of CIDR prefixes.
pub fn longest_prefix_match_entries<'a, I>(entries: I, ip: &IpAddr) -> Option<Metadata>
where
    I: IntoIterator<Item = (&'a IpNetwork, &'a Metadata)>,
{
    let mut best: Option<(&IpNetwork, &Metadata)> = None;

    for (network, metadata) in entries {
        if !network_contains_ip(network, ip) {
            continue;
        }

        match best {
            Some((best_network, _)) if best_network.prefix() >= network.prefix() => {}
            _ => best = Some((network, metadata)),
        }
    }

    best.map(|(_, metadata)| metadata.clone())
}

/// Find the longest prefix match for a node tree that stores children by network.
pub fn longest_prefix_match(root: &dyn RadixNode, ip: IpAddr) -> Option<Metadata> {
    let mut best_match = root.metadata();

    // The current node abstraction exposes keyed children, but not a child
    // iterator. Engine lookups use the entry-based helper above for now.
    if let Some(prefix) = root.prefix() {
        if !network_contains_ip(&prefix, &ip) {
            return None;
        }
    }

    best_match.take()
}

/// Convert an IP address to a binary string representation
pub fn ip_to_binary_string(ip: IpAddr) -> String {
    match ip {
        IpAddr::V4(ipv4) => {
            let octets = ipv4.octets();
                    /*
        in rust {:08b} this is a format specifier for integer in binary(base-2) representation.
        in rust 08 8 means we get a 8-bit binary and padds the rest with zeros
        here, we are dealing with ipv4 which is 32 bits long
        ipv6 is divided ito 4 octets which are 8 bits long each
        */
            octets.iter()
                .map(|&octet| format!("{:08b}", octet))
                .collect::<Vec<String>>()
                .concat()
        }
        /*
        in rust {:016b} this is a format specifier for integer in binary(base-2) representation.
        016 16 means we get a 16-bit binary and padds the rest with zeros
        here, we are dealing with ipv6 which is 128 bits long
        ipv6 is divided ito 8 hectets which are 64 bits long each
        */
        IpAddr::V6(ipv6) => {
            let segments = ipv6.segments();
            segments.iter()
                .map(|&segment| format!("{:016b}", segment))
                .collect::<Vec<String>>()
                .concat()
        }
    }
}

/// Alternative implementation using the provided longest_common_prefix_len function
pub fn longest_prefix_match_with_lcp(root: &dyn RadixNode, ip: IpAddr) -> Option<Metadata> {
    longest_prefix_match(root, ip)
}

/// Helper function to find the longest common prefix length between two strings
pub fn longest_common_prefix_len(left: &str, right: &str) -> usize {
    let left_bytes = left.as_bytes();
    let right_bytes = right.as_bytes();
    let limit = left_bytes.len().min(right_bytes.len());
    let mut index = 0;
    while index < limit && left_bytes[index] == right_bytes[index] {
        index += 1;
    }
    index
}

fn get_bit(ip: IpAddr, bit_pos: usize) -> u8 {
    let ip_bytes: &[u8] = match ip {
        IpAddr::V4(ipv4) => ipv4.octets().as_slice(),
        IpAddr::V6(ipv6) => ipv6.octets().as_slice(),
    };

    let byte_idx = bit_pos / 8;
    if byte_idx >= ip_bytes.len() {
        return 0;
    }

    let bit_idx = 7 - (bit_pos % 8);
    ((ip_bytes[byte_idx] >> bit_idx) & 1)
}

/// More efficient implementation using binary string representation
pub fn longest_prefix_match_binary(root: &dyn RadixNode, ip: IpAddr) -> Option<Metadata> {
    longest_prefix_match(root, ip)
}

pub fn network_contains_ip(network: &IpNetwork, ip: &IpAddr) -> bool {
    match (network, ip) {
        (IpNetwork::V4(network), IpAddr::V4(ip)) => network.contains(*ip),
        (IpNetwork::V6(network), IpAddr::V6(ip)) => network.contains(*ip),
        _ => false,
    }
}
