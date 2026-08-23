//!  (Patricia) radix trie node implementations.
//!
//! Unlike un nodes which store a single `bit`, Patricia nodes
//! store an `edge_bits: [u8;16]` and `edge_len: usize` representing a
//! multi-bit path segment. This collapses non-branching chains of bits
//! into a single node, giving O(k) lookups where k is the number of
//! branching points rather than the full prefix length.
//!
//! Four concurrency variants are provided (mirroring un):
//!
//! - [`NormalRadixNode`]   — RwLock on all fields  
//! - [`AtomicRadixNode`]   — AtomicU8 bit + RwLock for edge/children  
//! - [`PaddedRadixNode`]   — Cache-line padded Patricia node  
//! - [`LockFreeRadixNode`] — DashMap children for lock-free read throughput

use dashmap::DashMap;
use ipnetwork::IpNetwork;
use std::sync::{
    Arc, RwLock,
    atomic::{AtomicU8, Ordering},
};

use crate::traits::Node;
use crate::types::Metadata;

//
//  NORMAL NODE  (RwLock on every field)
//

#[derive(Default)]
#[repr(C, align(64))]
pub struct NormalRadixNode {
    /// Bit-string for the edge leading *into* this node (MSB-first, packed).
    edge_bits: RwLock<[u8; 16]>,
    /// Number of valid bits in `edge_bits` (may be < edge_bits.len()*8).
    edge_len: RwLock<usize>,
    /// Terminal metadata stored when this node represents a real prefix.
    metadata: RwLock<Option<Metadata>>,
    /// The IP network prefix that produced this node.
    prefix: RwLock<Option<IpNetwork>>,
    /// Left child  (next routing bit == 0).
    left: RwLock<Option<Arc<dyn Node>>>,
    /// Right child (next routing bit == 1).
    right: RwLock<Option<Arc<dyn Node>>>,
}

impl NormalRadixNode {
    pub fn new() -> Self {
        Self::default()
    }
}

impl Node for NormalRadixNode {
    // Shared methods

    /// Not meaningful for Patricia nodes; always returns `None`.
    fn bit(&self) -> Option<u8> {
        None
    }

    fn left(&self) -> Option<Arc<dyn Node>> {
        self.left.read().unwrap().clone()
    }

    fn right(&self) -> Option<Arc<dyn Node>> {
        self.right.read().unwrap().clone()
    }

    fn metadata(&self) -> Option<Metadata> {
        self.metadata.read().unwrap().clone()
    }

    fn prefix(&self) -> Option<IpNetwork> {
        *self.prefix.read().unwrap()
    }

    fn set_left(&self, node: Option<Arc<dyn Node>>) {
        *self.left.write().unwrap() = node;
    }

    fn set_right(&self, node: Option<Arc<dyn Node>>) {
        *self.right.write().unwrap() = node;
    }

    fn set_metadata(&self, metadata: Metadata) {
        *self.metadata.write().unwrap() = Some(metadata);
    }

    fn clear_metadata(&self) {
        *self.metadata.write().unwrap() = None;
    }

    /// Not used by Patricia trees; no-op.
    fn set_bit(&self, _bit: u8) {}

    fn set_prefix(&self, prefix: IpNetwork) {
        *self.prefix.write().unwrap() = Some(prefix);
    }

    // -node extensions

    fn edge_bits(&self) -> Option<[u8; 16]> {
        Some(self.edge_bits.read().unwrap().clone())
    }

    fn edge_len(&self) -> Option<usize> {
        Some(*self.edge_len.read().unwrap())
    }

    fn set_edge(&self, bits: [u8; 16], len: usize) {
        *self.edge_bits.write().unwrap() = bits;
        *self.edge_len.write().unwrap() = len;
    }
}

//
//  ATOMIC RADIX NODE
// (AtomicU8 encodes edge_len up to 254; RwLock for the bit-vector and children)
//

#[repr(C, align(64))]
pub struct AtomicRadixNode {
    /// Encodes edge_len: 0 = "empty/root", 1..=254 = actual length, 255 = overflow sentinel.
    /// For edge lengths ≥ 255 we fall back to the RwLock below.
    atomic_edge_len: AtomicU8,
    edge_bits: RwLock<[u8; 16]>,
    edge_len_overflow: RwLock<usize>, // used only when edge_len >= 255
    metadata: RwLock<Option<Metadata>>,
    prefix: RwLock<Option<IpNetwork>>,
    left: RwLock<Option<Arc<dyn Node>>>,
    right: RwLock<Option<Arc<dyn Node>>>,
}

impl AtomicRadixNode {
    pub fn new() -> Self {
        Self {
            atomic_edge_len: AtomicU8::new(0),
            edge_bits: RwLock::new([0u8; 16]),
            edge_len_overflow: RwLock::new(0),
            metadata: RwLock::new(None),
            prefix: RwLock::new(None),
            left: RwLock::new(None),
            right: RwLock::new(None),
        }
    }
}

impl Default for AtomicRadixNode {
    fn default() -> Self {
        Self::new()
    }
}

impl Node for AtomicRadixNode {
    fn bit(&self) -> Option<u8> {
        None
    }

    fn left(&self) -> Option<Arc<dyn Node>> {
        self.left.read().unwrap().clone()
    }

    fn right(&self) -> Option<Arc<dyn Node>> {
        self.right.read().unwrap().clone()
    }

    fn metadata(&self) -> Option<Metadata> {
        self.metadata.read().unwrap().clone()
    }

    fn prefix(&self) -> Option<IpNetwork> {
        *self.prefix.read().unwrap()
    }

    fn set_left(&self, node: Option<Arc<dyn Node>>) {
        *self.left.write().unwrap() = node;
    }

    fn set_right(&self, node: Option<Arc<dyn Node>>) {
        *self.right.write().unwrap() = node;
    }

    fn set_metadata(&self, metadata: Metadata) {
        *self.metadata.write().unwrap() = Some(metadata);
    }

    fn clear_metadata(&self) {
        *self.metadata.write().unwrap() = None;
    }

    fn set_bit(&self, _bit: u8) {}

    fn set_prefix(&self, prefix: IpNetwork) {
        *self.prefix.write().unwrap() = Some(prefix);
    }

    fn edge_bits(&self) -> Option<[u8; 16]> {
        Some(self.edge_bits.read().unwrap().clone())
    }

    fn edge_len(&self) -> Option<usize> {
        let atomic_val = self.atomic_edge_len.load(Ordering::Acquire) as usize;
        if atomic_val < 255 {
            Some(atomic_val)
        } else {
            // Overflow case: read from the RwLock
            Some(*self.edge_len_overflow.read().unwrap())
        }
    }

    fn set_edge(&self, bits: [u8; 16], len: usize) {
        *self.edge_bits.write().unwrap() = bits;
        if len < 255 {
            self.atomic_edge_len.store(len as u8, Ordering::Release);
        } else {
            // Mark overflow and store in RwLock
            self.atomic_edge_len.store(255, Ordering::Release);
            *self.edge_len_overflow.write().unwrap() = len;
        }
    }
}

//
//  PADDED RADIX NODE  (Cache-line padded Patricia node)
//

#[repr(C, align(64))]
pub struct PaddedRadixNode {
    edge_bits: RwLock<[u8; 16]>,
    _pad1: [u8; 63],
    edge_len: RwLock<usize>,
    metadata: RwLock<Option<Metadata>>,
    prefix: RwLock<Option<IpNetwork>>,
    left: RwLock<Option<Arc<dyn Node>>>,
    right: RwLock<Option<Arc<dyn Node>>>,
}

impl PaddedRadixNode {
    pub fn new() -> Self {
        Self {
            edge_bits: RwLock::new([0u8; 16]),
            _pad1: [0; 63],
            edge_len: RwLock::new(0),
            metadata: RwLock::new(None),
            prefix: RwLock::new(None),
            left: RwLock::new(None),
            right: RwLock::new(None),
        }
    }
}

impl Default for PaddedRadixNode {
    fn default() -> Self {
        Self::new()
    }
}

impl Node for PaddedRadixNode {
    fn bit(&self) -> Option<u8> {
        None
    }

    fn left(&self) -> Option<Arc<dyn Node>> {
        self.left.read().unwrap().clone()
    }

    fn right(&self) -> Option<Arc<dyn Node>> {
        self.right.read().unwrap().clone()
    }

    fn metadata(&self) -> Option<Metadata> {
        self.metadata.read().unwrap().clone()
    }

    fn prefix(&self) -> Option<IpNetwork> {
        *self.prefix.read().unwrap()
    }

    fn set_left(&self, node: Option<Arc<dyn Node>>) {
        *self.left.write().unwrap() = node;
    }

    fn set_right(&self, node: Option<Arc<dyn Node>>) {
        *self.right.write().unwrap() = node;
    }

    fn set_metadata(&self, metadata: Metadata) {
        *self.metadata.write().unwrap() = Some(metadata);
    }

    fn clear_metadata(&self) {
        *self.metadata.write().unwrap() = None;
    }

    fn set_bit(&self, _bit: u8) {}

    fn set_prefix(&self, prefix: IpNetwork) {
        *self.prefix.write().unwrap() = Some(prefix);
    }

    fn edge_bits(&self) -> Option<[u8; 16]> {
        Some(self.edge_bits.read().unwrap().clone())
    }

    fn edge_len(&self) -> Option<usize> {
        Some(*self.edge_len.read().unwrap())
    }

    fn set_edge(&self, bits: [u8; 16], len: usize) {
        *self.edge_bits.write().unwrap() = bits;
        *self.edge_len.write().unwrap() = len;
    }
}

//
//  LOCK-FREE RADIX NODE  (DashMap children + RwLock for edge data)
//

/// Key enum for the DashMap children store.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
enum ChildKey {
    Left,
    Right,
}
 
#[repr(C, align(64))]
pub struct LockFreeRadixNode {
    edge_bits: RwLock<[u8; 16]>,
    edge_len: RwLock<usize>,
    metadata: RwLock<Option<Metadata>>,
    prefix: RwLock<Option<IpNetwork>>,
    /// Lock-free child store: at most 2 entries (Left, Right).
    children: DashMap<ChildKey, Arc<dyn Node>>,
}

impl LockFreeRadixNode {
    pub fn new() -> Self {
        Self {
            edge_bits: RwLock::new([0u8; 16]),
            edge_len: RwLock::new(0),
            metadata: RwLock::new(None),
            prefix: RwLock::new(None),
            children: DashMap::new(),
        }
    }
}

impl Default for LockFreeRadixNode {
    fn default() -> Self {
        Self::new()
    }
}

impl Node for LockFreeRadixNode {
    fn bit(&self) -> Option<u8> {
        None
    }

    fn left(&self) -> Option<Arc<dyn Node>> {
        self.children
            .get(&ChildKey::Left)
            .map(|r| r.value().clone())
    }

    fn right(&self) -> Option<Arc<dyn Node>> {
        self.children
            .get(&ChildKey::Right)
            .map(|r| r.value().clone())
    }

    fn metadata(&self) -> Option<Metadata> {
        self.metadata.read().unwrap().clone()
    }

    fn prefix(&self) -> Option<IpNetwork> {
        *self.prefix.read().unwrap()
    }

    fn set_left(&self, node: Option<Arc<dyn Node>>) {
        match node {
            Some(n) => {
                self.children.insert(ChildKey::Left, n);
            }
            None => {
                self.children.remove(&ChildKey::Left);
            }
        }
    }

    fn set_right(&self, node: Option<Arc<dyn Node>>) {
        match node {
            Some(n) => {
                self.children.insert(ChildKey::Right, n);
            }
            None => {
                self.children.remove(&ChildKey::Right);
            }
        }
    }

    fn set_metadata(&self, metadata: Metadata) {
        *self.metadata.write().unwrap() = Some(metadata);
    }

    fn clear_metadata(&self) {
        *self.metadata.write().unwrap() = None;
    }

    fn set_bit(&self, _bit: u8) {}

    fn set_prefix(&self, prefix: IpNetwork) {
        *self.prefix.write().unwrap() = Some(prefix);
    }

    fn edge_bits(&self) -> Option<[u8; 16]> {
        Some(self.edge_bits.read().unwrap().clone())
    }

    fn edge_len(&self) -> Option<usize> {
        Some(*self.edge_len.read().unwrap())
    }

    fn set_edge(&self, bits: [u8; 16], len: usize) {
        *self.edge_bits.write().unwrap() = bits;
        *self.edge_len.write().unwrap() = len;
    }
}
