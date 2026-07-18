use std::net::IpAddr;
use std::sync::Arc;
use ipnetwork::IpNetwork;
use crate::lpm::{get_bit, longest_prefix_match_binary};
use crate::node::NodeBuilder;
use crate::traits::{NodeVariant, RadixNode, RouteTree};
use crate::types::Metadata;

#[derive(Clone)]
pub struct UncompressedTree {
    root: Arc<dyn RadixNode>,
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

// ============================================================
//  COMPRESSED TREE  (Patricia / Radix trie)
// ============================================================
//
// Each node stores a `prefix_bits: Vec<u8>` representing the
// compressed bit-string for that edge, instead of traversing
// one bit at a time. Non-branching chains of nodes are folded
// into a single node, giving O(k) lookups where k is the
// number of *branching points*, not the prefix length.
//
// On insert: if an existing edge partially matches the new
// prefix we split the node at the diverging bit, creating a
// new internal node and two children.
//
// On lookup: at each node we check that `prefix_bits` fully
// matches the corresponding bits of the query before moving
// to the next child.

use std::sync::{Mutex};

/// A single node in the compressed (Patricia) radix tree.
struct CompressedNode {
    /// The bit-string stored on the edge *into* this node.
    /// Bit 0 of prefix_bits[0] is the most-significant bit of that byte.
    edge_bits: Vec<u8>,
    /// Number of valid bits in edge_bits (may be < edge_bits.len()*8)
    edge_len: usize,
    /// Terminal data stored at this node (if this node represents a real prefix).
    metadata: Option<Metadata>,
    /// The network prefix that produced this node.
    prefix: Option<IpNetwork>,
    /// Left child (next bit == 0)
    left: Option<Arc<Mutex<CompressedNode>>>,
    /// Right child (next bit == 1)
    right: Option<Arc<Mutex<CompressedNode>>>,
}

impl CompressedNode {
    fn new_empty() -> Arc<Mutex<Self>> {
        Arc::new(Mutex::new(Self {
            edge_bits: Vec::new(),
            edge_len: 0,
            metadata: None,
            prefix: None,
            left: None,
            right: None,
        }))
    }

    fn new_leaf(edge_bits: Vec<u8>, edge_len: usize, prefix: IpNetwork, metadata: Metadata) -> Arc<Mutex<Self>> {
        Arc::new(Mutex::new(Self {
            edge_bits,
            edge_len,
            metadata: Some(metadata),
            prefix: Some(prefix),
            left: None,
            right: None,
        }))
    }
}

/// Extract bit `pos` from a byte slice (big-endian, MSB-first).
fn get_bit_from_bytes(bytes: &[u8], pos: usize) -> u8 {
    let byte_idx = pos / 8;
    let bit_idx = 7 - (pos % 8);
    if byte_idx >= bytes.len() { return 0; }
    (bytes[byte_idx] >> bit_idx) & 1
}

/// Extract up to `len` bits starting at `start` from `bytes` into a new Vec<u8>.
fn extract_bits(bytes: &[u8], start: usize, len: usize) -> Vec<u8> {
    let byte_count = (len + 7) / 8;
    let mut out = vec![0u8; byte_count];
    for i in 0..len {
        let b = get_bit_from_bytes(bytes, start + i);
        let byte_i = i / 8;
        let bit_i = 7 - (i % 8);
        out[byte_i] |= b << bit_i;
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

/// Convert an IpAddr to its canonical big-endian byte representation.
fn ip_to_bytes(ip: IpAddr) -> Vec<u8> {
    match ip {
        IpAddr::V4(v4) => v4.octets().to_vec(),
        IpAddr::V6(v6) => v6.octets().to_vec(),
    }
}

// ---- Public interface ----

#[derive(Clone)]
pub struct CompressedTree {
    root: Arc<Mutex<CompressedNode>>,
}

impl CompressedTree {
    pub fn new(_node_variant: NodeVariant) -> Self {
        // NodeVariant is accepted for API parity but the compressed tree
        // manages its own nodes internally via Arc<Mutex<>>.
        Self { root: CompressedNode::new_empty() }
    }

    /// Insert into the Patricia trie, splitting nodes as needed.
    fn insert_inner(
        node: &Arc<Mutex<CompressedNode>>,
        key: &[u8],
        key_len: usize,
        depth: usize,
        prefix: IpNetwork,
        metadata: Metadata,
    ) -> bool {
        let mut n = node.lock().unwrap();
        let remaining = key_len.saturating_sub(depth);

        if n.edge_len == 0 && n.metadata.is_none() && n.left.is_none() && n.right.is_none() {
            // Empty node: store directly
            n.edge_bits = extract_bits(key, depth, remaining);
            n.edge_len = remaining;
            n.prefix = Some(prefix);
            let is_new = n.metadata.is_none();
            n.metadata = Some(metadata);
            return is_new;
        }

        // How many bits of edge match the incoming key?
        let key_rem = extract_bits(key, depth, remaining);
        let shared = common_prefix_len(&n.edge_bits, n.edge_len, &key_rem, remaining);

        if shared == n.edge_len && shared == remaining {
            // Exact match — update metadata at this node
            let is_new = n.metadata.is_none();
            n.metadata = Some(metadata);
            n.prefix = Some(prefix);
            return is_new;
        }

        if shared < n.edge_len {
            // Partial match — split this node
            let pivot_bit = get_bit_from_bytes(&n.edge_bits, shared);

            // Create a new child carrying the remainder of the current edge
            let child_edge_bits = extract_bits(&n.edge_bits, shared + 1, n.edge_len - shared - 1);
            let child_edge_len = n.edge_len - shared - 1;
            let child = Arc::new(Mutex::new(CompressedNode {
                edge_bits: child_edge_bits,
                edge_len: child_edge_len,
                metadata: n.metadata.take(),
                prefix: n.prefix.take(),
                left: n.left.take(),
                right: n.right.take(),
            }));

            // Trim current node edge to shared prefix
            n.edge_bits = extract_bits(&n.edge_bits, 0, shared);
            n.edge_len = shared;

            // Wire child into appropriate side
            if pivot_bit == 0 {
                n.left = Some(child);
            } else {
                n.right = Some(child);
            }

            // Now place the new prefix in the other side (or here if shared == remaining)
            if shared == remaining {
                n.metadata = Some(metadata);
                n.prefix = Some(prefix);
                return true;
            }

            let new_bit = get_bit_from_bytes(&key_rem, shared);
            let new_leaf_edge = extract_bits(&key_rem, shared + 1, remaining - shared - 1);
            let new_leaf = CompressedNode::new_leaf(new_leaf_edge, remaining - shared - 1, prefix, metadata);
            if new_bit == 0 {
                n.left = Some(new_leaf);
            } else {
                n.right = Some(new_leaf);
            }
            return true;
        }

        // shared == edge_len but we still have bits left to place — descend
        let next_bit = get_bit_from_bytes(&key_rem, shared);
        drop(n); // release lock before recursing

        let mut n = node.lock().unwrap();
        let child_opt = if next_bit == 0 { n.left.clone() } else { n.right.clone() };
        drop(n);

        if let Some(child) = child_opt {
            Self::insert_inner(&child, key, key_len, depth + shared + 1, prefix, metadata)
        } else {
            // Create a new leaf child
            let new_depth = depth + shared + 1;
            let new_remaining = key_len.saturating_sub(new_depth);
            let leaf_edge = extract_bits(key, new_depth, new_remaining);
            let leaf = CompressedNode::new_leaf(leaf_edge, new_remaining, prefix, metadata);
            let mut n = node.lock().unwrap();
            if next_bit == 0 {
                n.left = Some(leaf);
            } else {
                n.right = Some(leaf);
            }
            true
        }
    }

    /// Walk the trie returning the most-specific matching prefix.
    fn lookup_inner(node: &Arc<Mutex<CompressedNode>>, key: &[u8], depth: usize) -> Option<Metadata> {
        let n = node.lock().unwrap();
        if n.edge_len == 0 && n.metadata.is_none() {
            return None;
        }

        let remaining = (key.len() * 8).saturating_sub(depth);
        let key_rem = extract_bits(key, depth, remaining);
        let shared = common_prefix_len(&n.edge_bits, n.edge_len, &key_rem, remaining);

        if shared < n.edge_len {
            // The edge doesn't fully match — no route here
            return None;
        }

        // Edge matched: record any terminal at this node, then keep descending
        let mut best = n.metadata.clone();
        let new_depth = depth + shared;

        if new_depth >= key.len() * 8 {
            return best;
        }

        let next_bit = get_bit_from_bytes(key, new_depth);
        let child_opt = if next_bit == 0 { n.left.clone() } else { n.right.clone() };
        drop(n);

        if let Some(child) = child_opt {
            if let Some(deeper) = Self::lookup_inner(&child, key, new_depth + 1) {
                best = Some(deeper);
            }
        }
        best
    }

    fn remove_inner(node: &Arc<Mutex<CompressedNode>>, key: &[u8], key_len: usize, depth: usize) -> Option<Metadata> {
        let n = node.lock().unwrap();
        let remaining = key_len.saturating_sub(depth);
        let key_rem = extract_bits(key, depth, remaining);
        let shared = common_prefix_len(&n.edge_bits, n.edge_len, &key_rem, remaining);
        drop(n);

        if shared < {let n2 = node.lock().unwrap(); n2.edge_len} {
            return None;
        }

        if shared == remaining {
            let mut n = node.lock().unwrap();
            let removed = n.metadata.take();
            n.prefix = None;
            return removed;
        }

        let n = node.lock().unwrap();
        let new_depth = depth + shared + 1;
        let next_bit = get_bit_from_bytes(key, depth + shared);
        let child_opt = if next_bit == 0 { n.left.clone() } else { n.right.clone() };
        drop(n);

        if let Some(child) = child_opt {
            Self::remove_inner(&child, key, key_len, new_depth)
        } else {
            None
        }
    }

    fn contains_inner(node: &Arc<Mutex<CompressedNode>>, key: &[u8], key_len: usize, depth: usize) -> bool {
        let n = node.lock().unwrap();
        let remaining = key_len.saturating_sub(depth);
        let key_rem = extract_bits(key, depth, remaining);
        let shared = common_prefix_len(&n.edge_bits, n.edge_len, &key_rem, remaining);

        if shared < n.edge_len {
            return false;
        }
        if shared == remaining {
            return n.metadata.is_some();
        }

        let next_bit = get_bit_from_bytes(key, depth + shared);
        let child_opt = if next_bit == 0 { n.left.clone() } else { n.right.clone() };
        drop(n);

        if let Some(child) = child_opt {
            Self::contains_inner(&child, key, key_len, depth + shared + 1)
        } else {
            false
        }
    }

    fn clear_inner(node: &Arc<Mutex<CompressedNode>>) {
        let mut n = node.lock().unwrap();
        n.edge_bits.clear();
        n.edge_len = 0;
        n.metadata = None;
        n.prefix = None;
        n.left = None;
        n.right = None;
    }
}

impl RouteTree for CompressedTree {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<bool, String> {
        let ip = prefix.network();
        let key = ip_to_bytes(ip);
        let key_len = prefix.prefix() as usize;
        let is_new = Self::insert_inner(&self.root, &key, key_len, 0, prefix, metadata);
        Ok(is_new)
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        let key = ip_to_bytes(*ip);
        let key_bits = key.len() * 8;
        Self::lookup_inner(&self.root, &key, 0).filter(|_| {
            // Validate the match actually covers the IP
            // (lookup_inner already handles this via prefix containment)
            true
        });
        // Re-run with full key length available
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
