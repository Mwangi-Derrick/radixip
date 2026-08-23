//! Route tree implementations.
//!
//! Two concrete [`RouteTree`] types are provided:
//!
//! - [`UncompressedTree`] — bit-by-bit binary trie; every edge is exactly 1 bit.
//! - [`CompressedTree`]   — Patricia / radix trie; each edge carries a variable-length
//!   bit string, skipping over non-branching paths for faster traversal.
//!
//! Both trees store `Arc<dyn Node>`, so the node variant (Normal, Atomic,
//! Padded, LockFree, or their Compressed equivalents) is chosen at construction
//! time via [`NodeBuilder`] and [`NodeVariant`].

use crate::lpm::{get_bit, longest_prefix_match_binary};
use crate::node::NodeBuilder;
use crate::traits::{Node, NodeVariant, RouteTree};
use crate::types::Metadata;
use ipnetwork::IpNetwork;
use std::net::IpAddr;
use std::sync::Arc;

//
// UNCOMPRESSED TREE  (bit-by-bit binary trie)
//

#[derive(Clone)]
pub struct UncompressedTree {
    root: Arc<dyn Node>,
    node_builder: NodeBuilder,
}

impl UncompressedTree {
    pub fn new(node_variant: NodeVariant) -> Self {
        let builder = NodeBuilder::new(node_variant);
        Self {
            root: builder.build(),
            node_builder: builder,
        }
    }
}

impl RouteTree for UncompressedTree {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<bool, String> {
        let ip = prefix.network();
        let prefix_len = prefix.prefix() as usize;

        let mut current_arc = self.root.clone();

        for depth in 0..prefix_len {
            let bit = get_bit(ip, depth);
            let next = if bit == 0 {
                current_arc.left()
            } else {
                current_arc.right()
            };

            match next {
                Some(node) => {
                    current_arc = node;
                }
                None => {
                    let new_node = self.node_builder.build();
                    if bit == 0 {
                        current_arc.set_left(Some(new_node.clone()));
                    } else {
                        current_arc.set_right(Some(new_node.clone()));
                    }
                    current_arc = new_node;
                }
            }
        }

        let is_new = current_arc.metadata().is_none();
        current_arc.set_prefix(prefix);
        current_arc.set_metadata(metadata);

        Ok(is_new)
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        longest_prefix_match_binary(&*self.root, *ip)
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        let ip = prefix.network();
        let prefix_len = prefix.prefix() as usize;

        let mut current_arc = self.root.clone();

        for depth in 0..prefix_len {
            let bit = get_bit(ip, depth);
            let next = if bit == 0 {
                current_arc.left()
            } else {
                current_arc.right()
            };
            match next {
                Some(node) => current_arc = node,
                None => return None,
            }
        }

        let removed = current_arc.metadata();
        if removed.is_some() {
            current_arc.clear_metadata();
        }

        removed
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        let ip = prefix.network();
        let prefix_len = prefix.prefix() as usize;

        let mut current_arc = self.root.clone();

        for depth in 0..prefix_len {
            let bit = get_bit(ip, depth);
            let next = if bit == 0 {
                current_arc.left()
            } else {
                current_arc.right()
            };
            match next {
                Some(node) => current_arc = node,
                None => return false,
            }
        }

        current_arc.metadata().is_some()
    }

    fn clear(&self) {
        self.root.set_left(None);
        self.root.set_right(None);
    }
}

//
// COMPRESSED TREE  (Patricia / radix trie)
//
// Each node stores an `edge_bits: [u8;16]` representing the compressed
// bit-string for that edge, instead of traversing one bit at a time.
// Non-branching chains of nodes are folded into a single node, giving
// O(k) lookups where k is the number of *branching points*, not the
// prefix length.
//
// On insert: if an existing edge partially matches the new prefix we split
// the node at the diverging bit, creating a new internal node and two children.
//
// On lookup: at each node we check that `edge_bits` fully matches the
// corresponding bits of the query before moving to the next child.
//
// The tree works with any node variant that implements the `edge_bits`,
// `edge_len`, and `set_edge` extensions of `Node`.  Compressed variants
// are picked via `NodeBuilder` from `NodeVariant::Compressed*`.
//

/// Helper to read edge fields from a node via the trait extension.
/// Returns (edge_bits_clone, edge_len) for a compressed node.
fn node_edge(node: &Arc<dyn Node>) -> ([u8; 16], usize) {
    let bits = node.edge_bits().unwrap_or_default();
    let len = node.edge_len().unwrap_or(0);
    (bits, len)
}

/// Extract bit `pos` from a byte slice (big-endian, MSB-first).
fn get_bit_from_bytes(bytes: &[u8], pos: usize) -> u8 {
    let byte_idx = pos / 8;
    let bit_idx = 7 - (pos % 8);
    if byte_idx >= bytes.len() {
        return 0;
    }
    (bytes[byte_idx] >> bit_idx) & 1
}

fn extract_bits(bytes: &[u8], start: usize, len: usize) -> [u8; 16] {
    let mut out = [0u8; 16]; // Fixed 16-byte array
    let max_len = len.min(128); // Max bits for 16 bytes
    for i in 0..max_len {
        let b = get_bit_from_bytes(bytes, start + i);
        let byte_i = i / 8;
        let bit_i = 7 - (i % 8);
        if byte_i < 16 {
            out[byte_i] |= b << bit_i;
        }
    }
    out
}

/// How many leading bits do two bit-arrays share?
fn common_prefix_len(a: &[u8], a_len: usize, b: &[u8], b_len: usize) -> usize {
    let max = a_len.min(b_len);
    for i in 0..max {
        if get_bit_from_bytes(a, i) != get_bit_from_bytes(b, i) {
            return i;
        }
    }
    max
}

fn ip_to_bytes(ip: IpAddr) -> [u8; 16] {
    match ip {
        IpAddr::V4(v4) => {
            let octets = v4.octets();
            let mut result = [0u8; 16];
            result[12..16].copy_from_slice(&octets); // IPv4 in last 4 bytes
            result
        }
        IpAddr::V6(v6) => v6.octets(),
    }
}

#[derive(Clone)]
pub struct CompressedTree {
    root: Arc<dyn Node>,
    node_builder: NodeBuilder,
}

impl CompressedTree {
    pub fn new(node_variant: NodeVariant) -> Self {
        // Ensure we use a compressed variant, upgrading if necessary.
        let variant = match node_variant {
            NodeVariant::NormalTrieNode | NodeVariant::NormalRadixNode => {
                NodeVariant::NormalRadixNode
            }
            NodeVariant::AtomicTrieNode | NodeVariant::AtomicRadixNode => {
                NodeVariant::AtomicRadixNode
            }
            NodeVariant::PaddedTrieNode | NodeVariant::PaddedRadixNode => {
                NodeVariant::PaddedRadixNode
            }
            NodeVariant::LockFreeTrieNode | NodeVariant::LockFreeRadixNode => {
                NodeVariant::LockFreeRadixNode
            }
        };
        let builder = NodeBuilder::new(variant);
        Self {
            root: builder.build(),
            node_builder: builder,
        }
    }

    /// Insert into the Patricia trie, splitting nodes as needed.
    fn insert_inner(
        node: &Arc<dyn Node>,
        node_builder: &NodeBuilder,
        key: &[u8],
        key_len: usize,
        depth: usize,
        prefix: IpNetwork,
        metadata: Metadata,
    ) -> bool {
        let (edge_bits, edge_len) = node_edge(node);
        let remaining = key_len.saturating_sub(depth);

        // Empty node: store directly
        if edge_len == 0
            && node.metadata().is_none()
            && node.left().is_none()
            && node.right().is_none()
        {
            let new_bits = extract_bits(key, depth, remaining);
            node.set_edge(new_bits, remaining);
            node.set_prefix(prefix);
            node.set_metadata(metadata);
            return true;
        }

        // How many bits of edge match the incoming key?
        let key_rem = extract_bits(key, depth, remaining);
        let shared = common_prefix_len(&edge_bits, edge_len, &key_rem, remaining);

        // Exact match — update metadata at this node.
        if shared == edge_len && shared == remaining {
            let is_new = node.metadata().is_none();
            node.set_metadata(metadata);
            node.set_prefix(prefix);
            return is_new;
        }

        // Partial match — split this node at the diverging bit.
        if shared < edge_len {
            let pivot_bit = get_bit_from_bytes(&edge_bits, shared);

            // Build a child carrying the remainder of the current edge.
            let child_edge_bits = extract_bits(&edge_bits, shared + 1, edge_len - shared - 1);
            let child_edge_len = edge_len - shared - 1;
            let child = node_builder.build();
            child.set_edge(child_edge_bits, child_edge_len);
            if let Some(m) = node.metadata() {
                child.set_metadata(m);
                node.clear_metadata();
            }
            if let Some(p) = node.prefix() {
                child.set_prefix(p);
            }

            // Move children of current node down to child.
            child.set_left(node.left());
            child.set_right(node.right());

            // Trim current node edge to shared prefix.
            let new_edge = extract_bits(&edge_bits, 0, shared);
            node.set_edge(new_edge, shared);
            node.set_left(None);
            node.set_right(None);

            // Wire child into the appropriate side.
            if pivot_bit == 0 {
                node.set_left(Some(child));
            } else {
                node.set_right(Some(child));
            }

            // Place the new prefix in the other side (or here if shared == remaining).
            if shared == remaining {
                node.set_metadata(metadata);
                node.set_prefix(prefix);
                return true;
            }

            let new_bit = get_bit_from_bytes(&key_rem, shared);
            let new_leaf_edge = extract_bits(&key_rem, shared + 1, remaining - shared - 1);
            let new_leaf = node_builder.build();
            new_leaf.set_edge(new_leaf_edge, remaining - shared - 1);
            new_leaf.set_prefix(prefix);
            new_leaf.set_metadata(metadata);

            if new_bit == 0 {
                node.set_left(Some(new_leaf));
            } else {
                node.set_right(Some(new_leaf));
            }
            return true;
        }

        // shared == edge_len but bits remain — descend into the correct child.
        let next_bit = get_bit_from_bytes(&key_rem, shared);

        let child_opt = if next_bit == 0 {
            node.left()
        } else {
            node.right()
        };

        if let Some(child) = child_opt {
            Self::insert_inner(
                &child,
                node_builder,
                key,
                key_len,
                depth + shared + 1,
                prefix,
                metadata,
            )
        } else {
            // Allocate a new leaf child.
            let new_depth = depth + shared + 1;
            let new_remaining = key_len.saturating_sub(new_depth);
            let leaf_edge = extract_bits(key, new_depth, new_remaining);
            let leaf = node_builder.build();
            leaf.set_edge(leaf_edge, new_remaining);
            leaf.set_prefix(prefix);
            leaf.set_metadata(metadata);

            if next_bit == 0 {
                node.set_left(Some(leaf));
            } else {
                node.set_right(Some(leaf));
            }
            true
        }
    }

    /// Walk the trie returning the most-specific matching prefix.
    fn lookup_inner(node: &Arc<dyn Node>, key: &[u8], depth: usize) -> Option<Metadata> {
        let (edge_bits, edge_len) = node_edge(node);

        if edge_len == 0 && node.metadata().is_none() {
            return None;
        }

        let remaining = (key.len() * 8).saturating_sub(depth);
        let key_rem = extract_bits(key, depth, remaining);
        let shared = common_prefix_len(&edge_bits, edge_len, &key_rem, remaining);

        // Edge doesn't fully match — no route here.
        if shared < edge_len {
            return None;
        }

        // Edge matched: record any terminal at this node, then keep descending.
        let mut best = node.metadata();
        let new_depth = depth + shared;

        if new_depth >= key.len() * 8 {
            return best;
        }

        let next_bit = get_bit_from_bytes(key, new_depth);
        let child_opt = if next_bit == 0 {
            node.left()
        } else {
            node.right()
        };

        if let Some(child) = child_opt {
            if let Some(deeper) = Self::lookup_inner(&child, key, new_depth + 1) {
                best = Some(deeper);
            }
        }
        best
    }

    fn remove_inner(
        node: &Arc<dyn Node>,
        key: &[u8],
        key_len: usize,
        depth: usize,
    ) -> Option<Metadata> {
        let (edge_bits, edge_len) = node_edge(node);
        let remaining = key_len.saturating_sub(depth);
        let key_rem = extract_bits(key, depth, remaining);
        let shared = common_prefix_len(&edge_bits, edge_len, &key_rem, remaining);

        if shared < edge_len {
            return None;
        }

        if shared == remaining {
            let removed = node.metadata();
            node.clear_metadata();
            return removed;
        }

        let next_bit = get_bit_from_bytes(key, depth + shared);
        let child_opt = if next_bit == 0 {
            node.left()
        } else {
            node.right()
        };

        if let Some(child) = child_opt {
            Self::remove_inner(&child, key, key_len, depth + shared + 1)
        } else {
            None
        }
    }

    fn contains_inner(node: &Arc<dyn Node>, key: &[u8], key_len: usize, depth: usize) -> bool {
        let (edge_bits, edge_len) = node_edge(node);
        let remaining = key_len.saturating_sub(depth);
        let key_rem = extract_bits(key, depth, remaining);
        let shared = common_prefix_len(&edge_bits, edge_len, &key_rem, remaining);

        if shared < edge_len {
            return false;
        }
        if shared == remaining {
            return node.metadata().is_some();
        }

        let next_bit = get_bit_from_bytes(key, depth + shared);
        let child_opt = if next_bit == 0 {
            node.left()
        } else {
            node.right()
        };

        if let Some(child) = child_opt {
            Self::contains_inner(&child, key, key_len, depth + shared + 1)
        } else {
            false
        }
    }

    fn clear_inner(node: &Arc<dyn Node>) {
        node.set_edge([0u8; 16], 0);
        node.clear_metadata();
        node.set_left(None);
        node.set_right(None);
    }
}

impl RouteTree for CompressedTree {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<bool, String> {
        let ip = prefix.network();
        let key = ip_to_bytes(ip);
        let key_len = prefix.prefix() as usize;
        let is_new = Self::insert_inner(
            &self.root,
            &self.node_builder,
            &key,
            key_len,
            0,
            prefix,
            metadata,
        );
        Ok(is_new)
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        let key = ip_to_bytes(*ip);
        Self::lookup_inner(&self.root, &key, 0)
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        let ip = prefix.network();
        let key = ip_to_bytes(ip);
        let key_len = prefix.prefix() as usize;
        Self::remove_inner(&self.root, &key, key_len, 0)
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        let ip = prefix.network();
        let key = ip_to_bytes(ip);
        let key_len = prefix.prefix() as usize;
        Self::contains_inner(&self.root, &key, key_len, 0)
    }

    fn clear(&self) {
        Self::clear_inner(&self.root);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::str::FromStr;

    fn create_metadata(label: &str) -> Metadata {
        Metadata::new(label.to_string())
    }

    //
    // COMPRESSED TREE TESTS
    //

    #[test]
    fn test_compressed_trie_v4() {
        let tree = CompressedTree::new(NodeVariant::NormalRadixNode);

        let prefix1 = IpNetwork::from_str("192.168.0.0/16").unwrap();
        let prefix2 = IpNetwork::from_str("192.168.1.0/24").unwrap();

        tree.insert(prefix1, create_metadata("local")).unwrap();
        tree.insert(prefix2, create_metadata("subnet")).unwrap();

        let ip = IpAddr::from_str("192.168.1.5").unwrap();
        assert_eq!(tree.lookup(&ip), Some(create_metadata("subnet")));
    }

    #[test]
    fn test_compressed_trie_v6() {
        let tree = CompressedTree::new(NodeVariant::NormalRadixNode);

        let prefix = IpNetwork::from_str("2001:db8::/32").unwrap();
        tree.insert(prefix, create_metadata("v6_network")).unwrap();

        let ip = IpAddr::from_str("2001:db8:1234::1").unwrap();
        assert!(tree.lookup(&ip).is_some());
    }

    #[test]
    fn test_compressed_all_variants() {
        for variant in [
            NodeVariant::NormalRadixNode,
            NodeVariant::AtomicRadixNode,
            NodeVariant::PaddedRadixNode,
            NodeVariant::LockFreeRadixNode,
        ] {
            let tree = CompressedTree::new(variant);
            let prefix = IpNetwork::from_str("10.0.0.0/8").unwrap();
            tree.insert(prefix, create_metadata("test")).unwrap();
            let ip = IpAddr::from_str("10.1.2.3").unwrap();
            assert_eq!(
                tree.lookup(&ip),
                Some(create_metadata("test")),
                "variant {:?} failed",
                variant
            );
        }
    }

    //
    // UNCOMPRESSED TREE TESTS
    //

    #[test]
    fn test_new_tree() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);
        let ip = IpAddr::from_str("192.168.1.1").unwrap();
        assert_eq!(tree.lookup(&ip), None);
    }

    #[test]
    fn test_insert_single_prefix_v4() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);
        let prefix = IpNetwork::from_str("192.168.0.0/16").unwrap();
        let metadata = create_metadata("local_network");

        let result = tree.insert(prefix, metadata.clone());
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), true);

        let ip = IpAddr::from_str("192.168.1.5").unwrap();
        assert_eq!(tree.lookup(&ip), Some(metadata.clone()));

        let ip_outside = IpAddr::from_str("10.0.0.1").unwrap();
        assert_eq!(tree.lookup(&ip_outside), None);
    }

    #[test]
    fn test_insert_single_prefix_v6() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);
        let prefix = IpNetwork::from_str("2001:db8::/32").unwrap();
        let metadata = create_metadata("ipv6_network");

        let result = tree.insert(prefix, metadata.clone());
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), true);

        let ip = IpAddr::from_str("2001:db8:1234::1").unwrap();
        assert_eq!(tree.lookup(&ip), Some(metadata.clone()));
    }

    //
    // LONGEST PREFIX MATCH TESTS
    //

    #[test]
    fn test_longest_prefix_match_v4() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);

        let prefix1 = IpNetwork::from_str("192.168.0.0/16").unwrap();
        let prefix2 = IpNetwork::from_str("192.168.1.0/24").unwrap();
        let prefix3 = IpNetwork::from_str("192.168.1.128/25").unwrap();

        let meta1 = create_metadata("network");
        let meta2 = create_metadata("subnet");
        let meta3 = create_metadata("subnet_half");

        tree.insert(prefix1, meta1.clone()).unwrap();
        tree.insert(prefix2, meta2.clone()).unwrap();
        tree.insert(prefix3, meta3.clone()).unwrap();

        let ip = IpAddr::from_str("192.168.1.200").unwrap();
        assert_eq!(tree.lookup(&ip), Some(meta3.clone()));

        let ip = IpAddr::from_str("192.168.1.50").unwrap();
        assert_eq!(tree.lookup(&ip), Some(meta2.clone()));

        let ip = IpAddr::from_str("192.168.2.1").unwrap();
        assert_eq!(tree.lookup(&ip), Some(meta1.clone()));
    }

    #[test]
    fn test_longest_prefix_match_v6() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);

        let prefix1 = IpNetwork::from_str("2001:db8::/32").unwrap();
        let prefix2 = IpNetwork::from_str("2001:db8:1234::/48").unwrap();
        let prefix3 = IpNetwork::from_str("2001:db8:1234:5678::/64").unwrap();

        let meta1 = create_metadata("network");
        let meta2 = create_metadata("subnet");
        let meta3 = create_metadata("host_network");

        tree.insert(prefix1, meta1.clone()).unwrap();
        tree.insert(prefix2, meta2.clone()).unwrap();
        tree.insert(prefix3, meta3.clone()).unwrap();

        let ip = IpAddr::from_str("2001:db8:1234:5678::1").unwrap();
        assert_eq!(tree.lookup(&ip), Some(meta3.clone()));

        let ip = IpAddr::from_str("2001:db8:1234:5679::1").unwrap();
        assert_eq!(tree.lookup(&ip), Some(meta2.clone()));

        let ip = IpAddr::from_str("2001:db8:5678::1").unwrap();
        assert_eq!(tree.lookup(&ip), Some(meta1.clone()));
    }

    //
    // REMOVE OPERATIONS TESTS
    //

    #[test]
    fn test_remove_existing_prefix() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);
        let prefix = IpNetwork::from_str("192.168.0.0/16").unwrap();
        let metadata = create_metadata("network");

        tree.insert(prefix, metadata.clone()).unwrap();

        let ip = IpAddr::from_str("192.168.1.1").unwrap();
        assert_eq!(tree.lookup(&ip), Some(metadata.clone()));

        let removed = tree.remove(&prefix);
        assert_eq!(removed, Some(metadata.clone()));

        assert_eq!(tree.lookup(&ip), None);
    }

    #[test]
    fn test_remove_nonexistent_prefix() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);
        let prefix = IpNetwork::from_str("192.168.0.0/16").unwrap();

        let removed = tree.remove(&prefix);
        assert_eq!(removed, None);
    }

    #[test]
    fn test_remove_with_overlapping_prefixes() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);

        let prefix1 = IpNetwork::from_str("192.168.0.0/16").unwrap();
        let prefix2 = IpNetwork::from_str("192.168.1.0/24").unwrap();

        let meta1 = create_metadata("network");
        let meta2 = create_metadata("subnet");

        tree.insert(prefix1, meta1.clone()).unwrap();
        tree.insert(prefix2, meta2.clone()).unwrap();

        let removed = tree.remove(&prefix2);
        assert_eq!(removed, Some(meta2.clone()));

        let ip = IpAddr::from_str("192.168.1.5").unwrap();
        assert_eq!(tree.lookup(&ip), Some(meta1.clone()));

        let removed = tree.remove(&prefix1);
        assert_eq!(removed, Some(meta1.clone()));

        assert_eq!(tree.lookup(&ip), None);
    }

    //
    // CONTAINS TESTS
    //

    #[test]
    fn test_contains() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);
        let prefix = IpNetwork::from_str("192.168.0.0/16").unwrap();
        let metadata = create_metadata("network");

        tree.insert(prefix, metadata).unwrap();

        assert!(tree.contains(&prefix));

        let different_prefix = IpNetwork::from_str("10.0.0.0/8").unwrap();
        assert!(!tree.contains(&different_prefix));
    }

    //
    // CLEAR TESTS
    //

    #[test]
    fn test_clear() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);

        let prefix1 = IpNetwork::from_str("192.168.0.0/16").unwrap();
        let prefix2 = IpNetwork::from_str("10.0.0.0/8").unwrap();

        tree.insert(prefix1, create_metadata("network1")).unwrap();
        tree.insert(prefix2, create_metadata("network2")).unwrap();

        let ip1 = IpAddr::from_str("192.168.1.1").unwrap();
        let ip2 = IpAddr::from_str("10.0.0.1").unwrap();
        assert!(tree.lookup(&ip1).is_some());
        assert!(tree.lookup(&ip2).is_some());

        tree.clear();

        assert!(tree.lookup(&ip1).is_none());
        assert!(tree.lookup(&ip2).is_none());
    }

    //
    // EDGE CASE TESTS
    //

    #[test]
    fn test_default_route() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);

        let default_v4 = IpNetwork::from_str("0.0.0.0/0").unwrap();
        let meta_default = create_metadata("default");
        tree.insert(default_v4, meta_default.clone()).unwrap();

        let ip1 = IpAddr::from_str("192.168.1.1").unwrap();
        let ip2 = IpAddr::from_str("10.0.0.1").unwrap();
        let ip3 = IpAddr::from_str("8.8.8.8").unwrap();

        assert_eq!(tree.lookup(&ip1), Some(meta_default.clone()));
        assert_eq!(tree.lookup(&ip2), Some(meta_default.clone()));
        assert_eq!(tree.lookup(&ip3), Some(meta_default.clone()));

        let default_v6 = IpNetwork::from_str("::/0").unwrap();
        let meta_v6_default = create_metadata("v6_default");
        tree.insert(default_v6, meta_v6_default.clone()).unwrap();

        let ip_v6 = IpAddr::from_str("2001:db8::1").unwrap();
        assert_eq!(tree.lookup(&ip_v6), Some(meta_v6_default.clone()));
    }

    #[test]
    fn test_host_address_32_prefix() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);

        let host_prefix = IpNetwork::from_str("192.168.1.100/32").unwrap();
        let meta_host = create_metadata("host");
        tree.insert(host_prefix, meta_host.clone()).unwrap();

        let exact_ip = IpAddr::from_str("192.168.1.100").unwrap();
        assert_eq!(tree.lookup(&exact_ip), Some(meta_host.clone()));

        let different_ip = IpAddr::from_str("192.168.1.101").unwrap();
        assert_eq!(tree.lookup(&different_ip), None);
    }

    #[test]
    fn test_insert_duplicate_prefix() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);
        let prefix = IpNetwork::from_str("192.168.0.0/16").unwrap();
        let meta1 = create_metadata("first");
        let meta2 = create_metadata("second");

        let result1 = tree.insert(prefix, meta1.clone());
        assert!(result1.is_ok());
        assert_eq!(result1.unwrap(), true);

        let result2 = tree.insert(prefix, meta2.clone());
        assert!(result2.is_ok());
        assert_eq!(result2.unwrap(), false);

        let ip = IpAddr::from_str("192.168.1.1").unwrap();
        assert_eq!(tree.lookup(&ip), Some(meta2));
    }

    #[test]
    fn test_multiple_inserts_different_prefixes() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);

        let prefixes = vec![
            ("192.168.0.0/16", "network_a"),
            ("10.0.0.0/8", "network_b"),
            ("172.16.0.0/12", "network_c"),
            ("192.168.1.0/24", "subnet_a"),
        ];

        for (prefix_str, label) in prefixes {
            let prefix = IpNetwork::from_str(prefix_str).unwrap();
            let result = tree.insert(prefix, create_metadata(label));
            assert!(result.is_ok());
            assert_eq!(result.unwrap(), true);
        }

        let test_cases = vec![
            ("192.168.1.5", "subnet_a"),
            ("192.168.2.1", "network_a"),
            ("10.1.1.1", "network_b"),
            ("172.16.1.1", "network_c"),
        ];

        for (ip_str, expected_label) in test_cases {
            let ip = IpAddr::from_str(ip_str).unwrap();
            let result = tree.lookup(&ip);
            assert_eq!(result, Some(create_metadata(expected_label)));
        }
    }

    //
    // MIXED IPv4 AND IPv6 TESTS
    //

    #[test]
    fn test_mixed_ipv4_ipv6() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);

        let v4_prefix = IpNetwork::from_str("192.168.0.0/16").unwrap();
        let v4_meta = create_metadata("v4_network");
        tree.insert(v4_prefix, v4_meta.clone()).unwrap();

        let v6_prefix = IpNetwork::from_str("2001:db8::/32").unwrap();
        let v6_meta = create_metadata("v6_network");
        tree.insert(v6_prefix, v6_meta.clone()).unwrap();

        let v4_ip = IpAddr::from_str("192.168.1.1").unwrap();
        assert_eq!(tree.lookup(&v4_ip), Some(v4_meta));

        let v6_ip = IpAddr::from_str("2001:db8:1234::1").unwrap();
        assert_eq!(tree.lookup(&v6_ip), Some(v6_meta));

        let wrong_ip = IpAddr::from_str("10.0.0.1").unwrap();
        assert_eq!(tree.lookup(&wrong_ip), None);
    }

    //
    // PERFORMANCE/BENCHMARK HELPER TESTS
    //

    #[test]
    fn test_large_number_of_routes() {
        let tree = UncompressedTree::new(NodeVariant::NormalTrieNode);

        for i in 0..100u32 {
            let prefix_str = format!("192.168.{}.0/24", i);
            let prefix = IpNetwork::from_str(&prefix_str).unwrap();
            let meta = create_metadata(&format!("subnet_{}", i));
            tree.insert(prefix, meta).unwrap();
        }

        for i in 0..100u32 {
            let ip_str = format!("192.168.{}.1", i);
            let ip = IpAddr::from_str(&ip_str).unwrap();
            let result = tree.lookup(&ip);
            assert!(result.is_some());
            assert_eq!(result.unwrap(), create_metadata(&format!("subnet_{}", i)));
        }
    }
}
