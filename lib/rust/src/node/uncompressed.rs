//! Uncompressed (bit-by-bit) trie node implementations.
//!
//! These nodes store a single `bit` value and left/right children,
//! traversed one bit at a time. There are three concurrency variants:
//! - [`NormalTrieNode`]  — RwLock on every field  
//! - [`AtomicTrieNode`]  — AtomicU8 for the bit, RwLock for children  
//! - [`PaddedTrieNode`]  — Cache-line padded to avoid false sharing  
//! - [`LockFreeTrieNode`] — DashMap-based children for fully lock-free reads  

use dashmap::DashMap;
use ipnetwork::IpNetwork;
use std::sync::{
    Arc, RwLock,
    atomic::{AtomicU8, Ordering},
};

use crate::traits::Node;
use crate::types::Metadata;

//
// NORMAL NODE (RwLock on all fields)
//

#[derive(Default)]
#[repr(C, align(64))]
pub struct NormalTrieNode {
    bit: RwLock<Option<u8>>,
    left: RwLock<Option<Arc<dyn Node>>>,
    right: RwLock<Option<Arc<dyn Node>>>,
    metadata: RwLock<Option<Metadata>>,
    prefix: RwLock<Option<IpNetwork>>,
}

impl NormalTrieNode {
    pub fn new() -> Self {
        Self::default()
    }
}

impl Node for NormalTrieNode {
    fn bit(&self) -> Option<u8> {
        *self.bit.read().unwrap()
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
#[repr(C, align(64))]
pub struct AtomicTrieNode {
    /// Encoded as 0 = None, n+1 = Some(n) so we can use AtomicU8.
    bit: AtomicU8,
    left: RwLock<Option<Arc<dyn Node>>>,
    right: RwLock<Option<Arc<dyn Node>>>,
    metadata: RwLock<Option<Metadata>>,
    prefix: RwLock<Option<IpNetwork>>,
}

impl AtomicTrieNode {
    pub fn new() -> Self {
        Self {
            bit: AtomicU8::new(0),
            left: RwLock::new(None),
            right: RwLock::new(None),
            metadata: RwLock::new(None),
            prefix: RwLock::new(None),
        }
    }

    pub fn with_bit(bit: u8) -> Self { // This function is not used anywhere in the provided context files.
        let node = Self::new();
        node.set_bit(bit);
        node
    }
}

impl Default for AtomicTrieNode {
    fn default() -> Self { // This impl should be for AtomicTrieNode
        Self::new()
    }
}

impl Node for AtomicTrieNode {
    fn bit(&self) -> Option<u8> { // This impl should be for AtomicTrieNode
        let b = self.bit.load(Ordering::Acquire);
        if b == 0 { None } else { Some(b - 1) }
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
pub struct PaddedTrieNode {
    bit: RwLock<Option<u8>>,
    _pad1: [u8; 63],
    left: RwLock<Option<Arc<dyn Node>>>,

    right: RwLock<Option<Arc<dyn Node>>>,

    metadata: RwLock<Option<Metadata>>,

    prefix: RwLock<Option<IpNetwork>>,
}

impl PaddedTrieNode {
    pub fn new() -> Self {
        Self {
            bit: RwLock::new(None),
            _pad1: [0; 63],
            left: RwLock::new(None),
            right: RwLock::new(None),
            metadata: RwLock::new(None),
            prefix: RwLock::new(None),
        }
    }
}

impl Default for PaddedTrieNode {
    fn default() -> Self {
        Self::new()
    }
}

impl Node for PaddedTrieNode {
    fn bit(&self) -> Option<u8> {
        *self.bit.read().unwrap()
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

#[repr(C, align(64))]
pub struct LockFreeTrieNode {
    bit: AtomicU8,
    children: DashMap<ChildKey, Arc<dyn Node>>,
    metadata: RwLock<Option<Metadata>>,
    prefix: RwLock<Option<IpNetwork>>,
}

impl LockFreeTrieNode {
    pub fn new() -> Self { // This impl should be for LockFreeTrieNode
        Self {
            bit: AtomicU8::new(0),
            children: DashMap::new(),
            metadata: RwLock::new(None),
            prefix: RwLock::new(None),
        }
    }
}

impl Default for LockFreeTrieNode {
    fn default() -> Self { // This impl should be for LockFreeTrieNode
        Self::new()
    }
}

impl Node for LockFreeTrieNode {
    fn bit(&self) -> Option<u8> { // This impl should be for LockFreeTrieNode
        let b = self.bit.load(Ordering::Acquire);
        if b == 0 { None } else { Some(b - 1) }
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

    fn set_bit(&self, bit: u8) {
        self.bit.store(bit + 1, Ordering::Release);
    }

    fn set_prefix(&self, prefix: IpNetwork) {
        *self.prefix.write().unwrap() = Some(prefix);
    }
}
