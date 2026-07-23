//! Uncompressed (bit-by-bit) radix trie node implementations.
//!
//! These nodes store a single `bit` value and left/right children,
//! traversed one bit at a time. There are three concurrency variants:
//!
//! - [`NormalNode`]  — RwLock on every field  
//! - [`AtomicNode`]  — AtomicU8 for the bit, RwLock for children  
//! - [`PaddedNode`]  — Cache-line padded to avoid false sharing  
//! - [`LockFreeNode`] — DashMap-based children for fully lock-free reads  

use dashmap::DashMap;
use ipnetwork::IpNetwork;
use std::sync::{
    Arc, RwLock,
    atomic::{AtomicU8, Ordering},
};

use crate::traits::RadixNode;
use crate::types::Metadata;

//
// NORMAL NODE (RwLock on all fields)
//

#[derive(Default)]
pub struct NormalNode {
    bit: RwLock<Option<u8>>,
    left: RwLock<Option<Arc<dyn RadixNode>>>,
    right: RwLock<Option<Arc<dyn RadixNode>>>,
    metadata: RwLock<Option<Metadata>>,
    prefix: RwLock<Option<IpNetwork>>,
}

impl NormalNode {
    pub fn new() -> Self {
        Self::default()
    }
}

impl RadixNode for NormalNode {
    fn bit(&self) -> Option<u8> {
        *self.bit.read().unwrap()
    }

    fn left(&self) -> Option<Arc<dyn RadixNode>> {
        self.left.read().unwrap().clone()
    }

    fn right(&self) -> Option<Arc<dyn RadixNode>> {
        self.right.read().unwrap().clone()
    }

    fn metadata(&self) -> Option<Metadata> {
        self.metadata.read().unwrap().clone()
    }

    fn prefix(&self) -> Option<IpNetwork> {
        *self.prefix.read().unwrap()
    }

    fn set_left(&self, node: Option<Arc<dyn RadixNode>>) {
        *self.left.write().unwrap() = node;
    }

    fn set_right(&self, node: Option<Arc<dyn RadixNode>>) {
        *self.right.write().unwrap() = node;
    }

    fn set_metadata(&self, metadata: Metadata) {
        *self.metadata.write().unwrap() = Some(metadata);
    }

    fn clear_metadata(&self) {
        *self.metadata.write().unwrap() = None;
    }

    fn set_bit(&self, bit: u8) {
        *self.bit.write().unwrap() = Some(bit);
    }

    fn set_prefix(&self, prefix: IpNetwork) {
        *self.prefix.write().unwrap() = Some(prefix);
    }
}

//
// ATOMIC NODE (AtomicU8 bit + RwLock children)
//

#[repr(C)]
pub struct AtomicNode {
    /// Encoded as 0 = None, n+1 = Some(n) so we can use AtomicU8.
    bit: AtomicU8,
    left: RwLock<Option<Arc<dyn RadixNode>>>,
    right: RwLock<Option<Arc<dyn RadixNode>>>,
    metadata: RwLock<Option<Metadata>>,
    prefix: RwLock<Option<IpNetwork>>,
}

impl AtomicNode {
    pub fn new() -> Self {
        Self {
            bit: AtomicU8::new(0),
            left: RwLock::new(None),
            right: RwLock::new(None),
            metadata: RwLock::new(None),
            prefix: RwLock::new(None),
        }
    }

    pub fn with_bit(bit: u8) -> Self {
        let node = Self::new();
        node.set_bit(bit);
        node
    }
}

impl Default for AtomicNode {
    fn default() -> Self {
        Self::new()
    }
}

impl RadixNode for AtomicNode {
    fn bit(&self) -> Option<u8> {
        let b = self.bit.load(Ordering::Acquire);
        if b == 0 { None } else { Some(b - 1) }
    }

    fn left(&self) -> Option<Arc<dyn RadixNode>> {
        self.left.read().unwrap().clone()
    }

    fn right(&self) -> Option<Arc<dyn RadixNode>> {
        self.right.read().unwrap().clone()
    }

    fn metadata(&self) -> Option<Metadata> {
        self.metadata.read().unwrap().clone()
    }

    fn prefix(&self) -> Option<IpNetwork> {
        *self.prefix.read().unwrap()
    }

    fn set_left(&self, node: Option<Arc<dyn RadixNode>>) {
        *self.left.write().unwrap() = node;
    }

    fn set_right(&self, node: Option<Arc<dyn RadixNode>>) {
        *self.right.write().unwrap() = node;
    }

    fn set_metadata(&self, metadata: Metadata) {
        *self.metadata.write().unwrap() = Some(metadata);
    }

    fn clear_metadata(&self) {
        *self.metadata.write().unwrap() = None;
    }

    fn set_bit(&self, bit: u8) {
        // Store bit+1 so 0 encodes None
        self.bit.store(bit + 1, Ordering::Release);
    }

    fn set_prefix(&self, prefix: IpNetwork) {
        *self.prefix.write().unwrap() = Some(prefix);
    }
}

//
// PADDED NODE (Cache-line aligned, 64-byte padding between fields)
//

#[repr(C, align(64))]
pub struct PaddedNode {
    _pad1: [u8; 64],
    bit: RwLock<Option<u8>>,
    _pad2: [u8; 56],
    left: RwLock<Option<Arc<dyn RadixNode>>>,
    _pad3: [u8; 56],
    right: RwLock<Option<Arc<dyn RadixNode>>>,
    _pad4: [u8; 56],
    metadata: RwLock<Option<Metadata>>,
    _pad5: [u8; 56],
    prefix: RwLock<Option<IpNetwork>>,
    _pad6: [u8; 56],
}

impl PaddedNode {
    pub fn new() -> Self {
        Self {
            bit: RwLock::new(None),
            left: RwLock::new(None),
            right: RwLock::new(None),
            metadata: RwLock::new(None),
            prefix: RwLock::new(None),
            _pad1: [0; 64],
            _pad2: [0; 56],
            _pad3: [0; 56],
            _pad4: [0; 56],
            _pad5: [0; 56],
            _pad6: [0; 56],
        }
    }
}

impl Default for PaddedNode {
    fn default() -> Self {
        Self::new()
    }
}

impl RadixNode for PaddedNode {
    fn bit(&self) -> Option<u8> {
        *self.bit.read().unwrap()
    }

    fn left(&self) -> Option<Arc<dyn RadixNode>> {
        self.left.read().unwrap().clone()
    }

    fn right(&self) -> Option<Arc<dyn RadixNode>> {
        self.right.read().unwrap().clone()
    }

    fn metadata(&self) -> Option<Metadata> {
        self.metadata.read().unwrap().clone()
    }

    fn prefix(&self) -> Option<IpNetwork> {
        *self.prefix.read().unwrap()
    }

    fn set_left(&self, node: Option<Arc<dyn RadixNode>>) {
        *self.left.write().unwrap() = node;
    }

    fn set_right(&self, node: Option<Arc<dyn RadixNode>>) {
        *self.right.write().unwrap() = node;
    }

    fn set_metadata(&self, metadata: Metadata) {
        *self.metadata.write().unwrap() = Some(metadata);
    }

    fn clear_metadata(&self) {
        *self.metadata.write().unwrap() = None;
    }

    fn set_bit(&self, bit: u8) {
        *self.bit.write().unwrap() = Some(bit);
    }

    fn set_prefix(&self, prefix: IpNetwork) {
        *self.prefix.write().unwrap() = Some(prefix);
    }
}

//
// LOCK-FREE NODE (DashMap-based, for maximum concurrent read throughput)
//

/// Key enum used inside the DashMap for left/right children.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
enum ChildKey {
    Left,
    Right,
}

pub struct LockFreeNode {
    bit: AtomicU8,
    children: DashMap<ChildKey, Arc<dyn RadixNode>>,
    metadata: RwLock<Option<Metadata>>,
    prefix: RwLock<Option<IpNetwork>>,
}

impl LockFreeNode {
    pub fn new() -> Self {
        Self {
            bit: AtomicU8::new(0),
            children: DashMap::new(),
            metadata: RwLock::new(None),
            prefix: RwLock::new(None),
        }
    }
}

impl Default for LockFreeNode {
    fn default() -> Self {
        Self::new()
    }
}

impl RadixNode for LockFreeNode {
    fn bit(&self) -> Option<u8> {
        let b = self.bit.load(Ordering::Acquire);
        if b == 0 { None } else { Some(b - 1) }
    }

    fn left(&self) -> Option<Arc<dyn RadixNode>> {
        self.children
            .get(&ChildKey::Left)
            .map(|r| r.value().clone())
    }

    fn right(&self) -> Option<Arc<dyn RadixNode>> {
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

    fn set_left(&self, node: Option<Arc<dyn RadixNode>>) {
        match node {
            Some(n) => {
                self.children.insert(ChildKey::Left, n);
            }
            None => {
                self.children.remove(&ChildKey::Left);
            }
        }
    }

    fn set_right(&self, node: Option<Arc<dyn RadixNode>>) {
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

    fn set_bit(&self, bit: u8) {
        self.bit.store(bit + 1, Ordering::Release);
    }

    fn set_prefix(&self, prefix: IpNetwork) {
        *self.prefix.write().unwrap() = Some(prefix);
    }
}
