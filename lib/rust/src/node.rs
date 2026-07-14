//! Radix tree node implementations with different characteristics

use std::sync::{Arc, RwLock, Mutex};
use std::sync::atomic::{AtomicU8, AtomicPtr, Ordering};
use std::collections::HashMap;
use std::ptr;
use dashmap::DashMap;

use crate::traits::*;

// NORMAL NODE (Mutex-based)

#[derive(Default)]
pub struct NormalNode {
    bit: Option<u8>,
    left: Option<Arc<RwLock<NormalNode>>>,
    right: Option<Arc<RwLock<NormalNode>>>,
    metadata: Option<Metadata>,
    prefix: Option<IpNetwork>,
    children: HashMap<IpNetwork, Arc<RwLock<NormalNode>>>,
}

impl RadixNode for NormalNode {
    fn bit(&self) -> Option<u8> {
        self.bit
    }
    
    fn left(&self) -> Option<Arc<dyn RadixNode>> {
        self.left.as_ref().map(|n| n.clone() as Arc<dyn RadixNode>)
    }
    
    fn right(&self) -> Option<Arc<dyn RadixNode>> {
        self.right.as_ref().map(|n| n.clone() as Arc<dyn RadixNode>)
    }
    
    fn metadata(&self) -> Option<&Metadata> {
        self.metadata.as_ref()
    }
    
    fn prefix(&self) -> Option<&IpNetwork> {
        self.prefix.as_ref()
    }
    
    fn get_child(&self, network: &IpNetwork) -> Option<Arc<dyn RadixNode>> {
        self.children
            .get(network)
            .map(|n| n.clone() as Arc<dyn RadixNode>)
    }
    
    fn insert_child(&self, network: IpNetwork, node: Arc<dyn RadixNode>) {
        // This is tricky with interior mutability
        // Need to use RwLock or similar
        unimplemented!("Use interior mutability or different design")
    }
    
    fn remove_child(&self, network: &IpNetwork) -> Option<Arc<dyn RadixNode>> {
        unimplemented!()
    }
    
    fn set_metadata(&self, metadata: Metadata) {
        unimplemented!()
    }
    
    fn clear_metadata(&self) {
        unimplemented!()
    }
}

// ATOMIC NODE (Lock-free)

#[repr(C)]
pub struct AtomicNode {
    bit: AtomicU8,                           // 0 = none, 1-2 = bit values
    left: AtomicPtr<AtomicNode>,            // Child for bit 0
    right: AtomicPtr<AtomicNode>,           // Child for bit 1
    metadata: AtomicPtr<Metadata>,          // Terminal data
    prefix: AtomicPtr<IpNetwork>,           // Associated prefix
    children: DashMap<IpNetwork, Arc<AtomicNode>>,
}

impl AtomicNode {
    pub fn new() -> Self {
        Self {
            bit: AtomicU8::new(0),
            left: AtomicPtr::new(ptr::null_mut()),
            right: AtomicPtr::new(ptr::null_mut()),
            metadata: AtomicPtr::new(ptr::null_mut()),
            prefix: AtomicPtr::new(ptr::null_mut()),
            children: DashMap::new(),
        }
    }
    
    pub fn with_bit(bit: u8) -> Self {
        Self {
            bit: AtomicU8::new(bit + 1), // 1-indexed to distinguish from 0
            left: AtomicPtr::new(ptr::null_mut()),
            right: AtomicPtr::new(ptr::null_mut()),
            metadata: AtomicPtr::new(ptr::null_mut()),
            prefix: AtomicPtr::new(ptr::null_mut()),
            children: DashMap::new(),
        }
    }
}

impl RadixNode for AtomicNode {
    fn bit(&self) -> Option<u8> {
        let b = self.bit.load(Ordering::Acquire);
        if b == 0 { None } else { Some(b - 1) }
    }
    
    fn left(&self) -> Option<Arc<dyn RadixNode>> {
        let ptr = self.left.load(Ordering::Acquire);
        if ptr.is_null() {
            None
        } else {
            unsafe { Some(Arc::from_raw(ptr) as Arc<dyn RadixNode>) }
        }
    }
    
    fn right(&self) -> Option<Arc<dyn RadixNode>> {
        let ptr = self.right.load(Ordering::Acquire);
        if ptr.is_null() {
            None
        } else {
            unsafe { Some(Arc::from_raw(ptr) as Arc<dyn RadixNode>) }
        }
    }
    
    fn metadata(&self) -> Option<&Metadata> {
        let ptr = self.metadata.load(Ordering::Acquire);
        if ptr.is_null() {
            None
        } else {
            unsafe { Some(&*ptr) }
        }
    }
    
    fn prefix(&self) -> Option<&IpNetwork> {
        let ptr = self.prefix.load(Ordering::Acquire);
        if ptr.is_null() {
            None
        } else {
            unsafe { Some(&*ptr) }
        }
    }
    
    fn get_child(&self, network: &IpNetwork) -> Option<Arc<dyn RadixNode>> {
        self.children
            .get(network)
            .map(|entry| entry.value().clone() as Arc<dyn RadixNode>)
    }
    
    fn insert_child(&self, network: IpNetwork, node: Arc<dyn RadixNode>) {
        if let Some(node) = node.downcast_ref::<AtomicNode>() {
            self.children.insert(network, node.clone());
        }
    }
    
    fn remove_child(&self, network: &IpNetwork) -> Option<Arc<dyn RadixNode>> {
        self.children
            .remove(network)
            .map(|(_, node)| node as Arc<dyn RadixNode>)
    }
    
    fn set_metadata(&self, metadata: Metadata) {
        let boxed = Box::new(metadata);
        let ptr = Box::into_raw(boxed);
        let old = self.metadata.swap(ptr, Ordering::Release);
        if !old.is_null() {
            unsafe { drop(Box::from_raw(old)) }
        }
    }
    
    fn clear_metadata(&self) {
        let old = self.metadata.swap(ptr::null_mut(), Ordering::Release);
        if !old.is_null() {
            unsafe { drop(Box::from_raw(old)) }
        }
    }
}

impl Drop for AtomicNode {
    fn drop(&mut self) {
        // Clean up any allocated data
        let meta = self.metadata.load(Ordering::Acquire);
        if !meta.is_null() {
            unsafe { drop(Box::from_raw(meta)) }
        }
        
        let prefix = self.prefix.load(Ordering::Acquire);
        if !prefix.is_null() {
            unsafe { drop(Box::from_raw(prefix)) }
        }
        
        // Children are handled by DashMap's Drop
    }
}

unsafe impl Send for AtomicNode {}
unsafe impl Sync for AtomicNode {}

// ============ PADDED NODE (Cache-line aligned) ============

#[repr(C, align(64))] // 64-byte cache line alignment
pub struct PaddedNode {
    // Each field on its own cache line to avoid false sharing
    _pad1: [u8; 64],
    bit: Option<u8>,
    _pad2: [u8; 63],
    left: Option<Arc<RwLock<PaddedNode>>>,
    _pad3: [u8; 56],
    right: Option<Arc<RwLock<PaddedNode>>>,
    _pad4: [u8; 56],
    metadata: Option<Metadata>,
    _pad5: [u8; 56],
    prefix: Option<IpNetwork>,
    _pad6: [u8; 56],
    children: HashMap<IpNetwork, Arc<RwLock<PaddedNode>>>,
    _pad7: [u8; 56],
}

impl RadixNode for PaddedNode {
    // Same implementation as NormalNode but with padding
    fn bit(&self) -> Option<u8> { self.bit }
    fn left(&self) -> Option<Arc<dyn RadixNode>> {
        self.left.as_ref().map(|n| n.clone() as Arc<dyn RadixNode>)
    }
    fn right(&self) -> Option<Arc<dyn RadixNode>> {
        self.right.as_ref().map(|n| n.clone() as Arc<dyn RadixNode>)
    }
    fn metadata(&self) -> Option<&Metadata> { self.metadata.as_ref() }
    fn prefix(&self) -> Option<&IpNetwork> { self.prefix.as_ref() }
    fn get_child(&self, network: &IpNetwork) -> Option<Arc<dyn RadixNode>> {
        self.children
            .get(network)
            .map(|n| n.clone() as Arc<dyn RadixNode>)
    }
    fn insert_child(&self, network: IpNetwork, node: Arc<dyn RadixNode>) {
        unimplemented!()
    }
    fn remove_child(&self, network: &IpNetwork) -> Option<Arc<dyn RadixNode>> {
        unimplemented!()
    }
    fn set_metadata(&self, metadata: Metadata) {
        unimplemented!()
    }
    fn clear_metadata(&self) {
        unimplemented!()
    }
}

// ============ NODE WRAPPER ENUM ============

pub enum NodeWrapper {
    Normal(Arc<RwLock<NormalNode>>),
    Atomic(Arc<AtomicNode>),
    Padded(Arc<RwLock<PaddedNode>>),
}

impl RadixNode for NodeWrapper {
    fn bit(&self) -> Option<u8> {
        match self {
            NodeWrapper::Normal(n) => n.read().unwrap().bit(),
            NodeWrapper::Atomic(n) => n.bit(),
            NodeWrapper::Padded(n) => n.read().unwrap().bit(),
        }
    }
    
    fn left(&self) -> Option<Arc<dyn RadixNode>> {
        match self {
            NodeWrapper::Normal(n) => n.read().unwrap().left(),
            NodeWrapper::Atomic(n) => n.left(),
            NodeWrapper::Padded(n) => n.read().unwrap().left(),
        }
    }
    
    fn right(&self) -> Option<Arc<dyn RadixNode>> {
        match self {
            NodeWrapper::Normal(n) => n.read().unwrap().right(),
            NodeWrapper::Atomic(n) => n.right(),
            NodeWrapper::Padded(n) => n.read().unwrap().right(),
        }
    }
    
    fn metadata(&self) -> Option<&Metadata> {
        match self {
            NodeWrapper::Normal(n) => n.read().unwrap().metadata(),
            NodeWrapper::Atomic(n) => n.metadata(),
            NodeWrapper::Padded(n) => n.read().unwrap().metadata(),
        }
    }
    
    fn prefix(&self) -> Option<&IpNetwork> {
        match self {
            NodeWrapper::Normal(n) => n.read().unwrap().prefix(),
            NodeWrapper::Atomic(n) => n.prefix(),
            NodeWrapper::Padded(n) => n.read().unwrap().prefix(),
        }
    }
    
    fn get_child(&self, network: &IpNetwork) -> Option<Arc<dyn RadixNode>> {
        match self {
            NodeWrapper::Normal(n) => n.read().unwrap().get_child(network),
            NodeWrapper::Atomic(n) => n.get_child(network),
            NodeWrapper::Padded(n) => n.read().unwrap().get_child(network),
        }
    }
    
    fn insert_child(&self, network: IpNetwork, node: Arc<dyn RadixNode>) {
        match self {
            NodeWrapper::Normal(n) => n.write().unwrap().insert_child(network, node),
            NodeWrapper::Atomic(n) => n.insert_child(network, node),
            NodeWrapper::Padded(n) => n.write().unwrap().insert_child(network, node),
        }
    }
    
    fn remove_child(&self, network: &IpNetwork) -> Option<Arc<dyn RadixNode>> {
        match self {
            NodeWrapper::Normal(n) => n.write().unwrap().remove_child(network),
            NodeWrapper::Atomic(n) => n.remove_child(network),
            NodeWrapper::Padded(n) => n.write().unwrap().remove_child(network),
        }
    }
    
    fn set_metadata(&self, metadata: Metadata) {
        match self {
            NodeWrapper::Normal(n) => n.write().unwrap().set_metadata(metadata),
            NodeWrapper::Atomic(n) => n.set_metadata(metadata),
            NodeWrapper::Padded(n) => n.write().unwrap().set_metadata(metadata),
        }
    }
    
    fn clear_metadata(&self) {
        match self {
            NodeWrapper::Normal(n) => n.write().unwrap().clear_metadata(),
            NodeWrapper::Atomic(n) => n.clear_metadata(),
            NodeWrapper::Padded(n) => n.write().unwrap().clear_metadata(),
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
    
    pub fn build(&self) -> Box<dyn RadixNode> {
        match self.variant {
            NodeVariant::Normal => {
                Box::new(NodeWrapper::Normal(Arc::new(RwLock::new(NormalNode::default()))))
            }
            NodeVariant::Atomic => {
                Box::new(NodeWrapper::Atomic(Arc::new(AtomicNode::new())))
            }
            NodeVariant::Padded => {
                Box::new(NodeWrapper::Padded(Arc::new(RwLock::new(PaddedNode {
                    bit: None,
                    left: None,
                    right: None,
                    metadata: None,
                    prefix: None,
                    children: HashMap::new(),
                    _pad1: [0; 64],
                    _pad2: [0; 63],
                    _pad3: [0; 56],
                    _pad4: [0; 56],
                    _pad5: [0; 56],
                    _pad6: [0; 56],
                    _pad7: [0; 56],
                }))))
            }
            NodeVariant::LockFree => {
                // Use atomic with epoch-based reclamation (simplified)
                Box::new(NodeWrapper::Atomic(Arc::new(AtomicNode::new())))
            }
        }
    }
    
    pub fn build_leaf(&self, network: IpNetwork, metadata: Metadata) -> Box<dyn RadixNode> {
        let node = self.build();
        node.insert_child(network, node.clone());
        node.set_metadata(metadata);
        node
    }
}