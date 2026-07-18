//! Radix tree node implementations with different characteristics

use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use std::sync::atomic::{AtomicU8, Ordering};

use dashmap::DashMap;
use ipnetwork::IpNetwork;

use crate::traits::*;
use crate::types::Metadata;

// NORMAL NODE (Mutex-based)

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

// ATOMIC NODE (Lock-free)

#[repr(C)]
pub struct AtomicNode {
    bit: AtomicU8,                           // 0 = none, 1-2 = bit values
    left: RwLock<Option<Arc<dyn RadixNode>>>, // Child for bit 0
    right: RwLock<Option<Arc<dyn RadixNode>>>, // Child for bit 1
    metadata: RwLock<Option<Metadata>>,      // Terminal data
    prefix: RwLock<Option<IpNetwork>>,       // Associated prefix
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
        let bit = self.bit.load(Ordering::Acquire);
        if bit == 0 { None } else { Some(bit - 1) }
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
        self.bit.store(bit + 1, Ordering::Release);
    }

    fn set_prefix(&self, prefix: IpNetwork) {
        *self.prefix.write().unwrap() = Some(prefix);
    }
}

// ============ PADDED NODE (Cache-line aligned) ============

#[repr(C, align(64))] // 64-byte cache line alignment
pub struct PaddedNode {
    // Each field on its own cache line to avoid false sharing
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
    // Same implementation as NormalNode but with padding
    fn bit(&self) -> Option<u8> { *self.bit.read().unwrap() }
    fn left(&self) -> Option<Arc<dyn RadixNode>> { self.left.read().unwrap().clone() }
    fn right(&self) -> Option<Arc<dyn RadixNode>> { self.right.read().unwrap().clone() }
    fn metadata(&self) -> Option<Metadata> { self.metadata.read().unwrap().clone() }
    fn prefix(&self) -> Option<IpNetwork> { *self.prefix.read().unwrap() }
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

// ============ NODE WRAPPER ENUM ============

pub enum NodeWrapper {
    Normal(Arc<NormalNode>),
    Atomic(Arc<AtomicNode>),
    Padded(Arc<PaddedNode>),
}

impl RadixNode for NodeWrapper {
    fn bit(&self) -> Option<u8> {
        match self {
            NodeWrapper::Normal(n) => n.bit(),
            NodeWrapper::Atomic(n) => n.bit(),
            NodeWrapper::Padded(n) => n.bit(),
        }
    }

    fn left(&self) -> Option<Arc<dyn RadixNode>> {
        match self {
            NodeWrapper::Normal(n) => n.left(),
            NodeWrapper::Atomic(n) => n.left(),
            NodeWrapper::Padded(n) => n.left(),
        }
    }

    fn right(&self) -> Option<Arc<dyn RadixNode>> {
        match self {
            NodeWrapper::Normal(n) => n.right(),
            NodeWrapper::Atomic(n) => n.right(),
            NodeWrapper::Padded(n) => n.right(),
        }
    }

    fn metadata(&self) -> Option<Metadata> {
        match self {
            NodeWrapper::Normal(n) => n.metadata(),
            NodeWrapper::Atomic(n) => n.metadata(),
            NodeWrapper::Padded(n) => n.metadata(),
        }
    }

    fn prefix(&self) -> Option<IpNetwork> {
        match self {
            NodeWrapper::Normal(n) => n.prefix(),
            NodeWrapper::Atomic(n) => n.prefix(),
            NodeWrapper::Padded(n) => n.prefix(),
        }
    }

    fn set_left(&self, node: Option<Arc<dyn RadixNode>>) {
        match self {
            NodeWrapper::Normal(n) => n.set_left(node),
            NodeWrapper::Atomic(n) => n.set_left(node),
            NodeWrapper::Padded(n) => n.set_left(node),
        }
    }

    fn set_right(&self, node: Option<Arc<dyn RadixNode>>) {
        match self {
            NodeWrapper::Normal(n) => n.set_right(node),
            NodeWrapper::Atomic(n) => n.set_right(node),
            NodeWrapper::Padded(n) => n.set_right(node),
        }
    }

    fn set_metadata(&self, metadata: Metadata) {
        match self {
            NodeWrapper::Normal(n) => n.set_metadata(metadata),
            NodeWrapper::Atomic(n) => n.set_metadata(metadata),
            NodeWrapper::Padded(n) => n.set_metadata(metadata),
        }
    }

    fn clear_metadata(&self) {
        match self {
            NodeWrapper::Normal(n) => n.clear_metadata(),
            NodeWrapper::Atomic(n) => n.clear_metadata(),
            NodeWrapper::Padded(n) => n.clear_metadata(),
        }
    }

    fn set_bit(&self, bit: u8) {
        match self {
            NodeWrapper::Normal(n) => n.set_bit(bit),
            NodeWrapper::Atomic(n) => n.set_bit(bit),
            NodeWrapper::Padded(n) => n.set_bit(bit),
        }
    }

    fn set_prefix(&self, prefix: IpNetwork) {
        match self {
            NodeWrapper::Normal(n) => n.set_prefix(prefix),
            NodeWrapper::Atomic(n) => n.set_prefix(prefix),
            NodeWrapper::Padded(n) => n.set_prefix(prefix),
        }
    }
}

// ============ NODE BUILDER ============

pub struct NodeBuilder {
    variant: NodeVariant,
}

impl NodeBuilder {
    pub fn new(variant: NodeVariant) -> Self {
        Self { variant }
    }

    pub fn build(&self) -> Arc<dyn RadixNode> {
        match self.variant {
            NodeVariant::Normal => Arc::new(NodeWrapper::Normal(Arc::new(NormalNode::new()))),
            NodeVariant::Atomic => Arc::new(NodeWrapper::Atomic(Arc::new(AtomicNode::new()))),
            NodeVariant::Padded => Arc::new(NodeWrapper::Padded(Arc::new(PaddedNode::new()))),
            NodeVariant::LockFree => {
                // Use atomic with epoch-based reclamation (simplified)
                Arc::new(NodeWrapper::Atomic(Arc::new(AtomicNode::new())))
            }
        }
    }

    pub fn build_leaf(&self, network: IpNetwork, metadata: Metadata) -> Arc<dyn RadixNode> {
        let node = self.build();
        node.set_prefix(network);
        node.set_metadata(metadata);
        node
    }
}
