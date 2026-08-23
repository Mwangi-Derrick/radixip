//! Node module for RadixIP.
//!
//! This module is split into two sub-modules:
//!
//! - [`uncompressed`] — bit-by-bit trie nodes (Normal, Atomic, Padded, LockFree)
//! - [`compressed`]   — Patricia trie nodes (CompressedNormal, CompressedAtomic,
//!                       CompressedPadded, CompressedLockFree)
//!
//! Public API is re-exported from this module so callers just do:
//!
//! ```rust
//! use crate::node::{NodeBuilder, NodeWrapper};
//! ```

pub mod compressed;
pub mod uncompressed;

pub use compressed::{
    AtomicRadixNode, LockFreeRadixNode, NormalRadixNode, PaddedRadixNode,
};
pub use uncompressed::{AtomicTrieNode, LockFreeTrieNode, NormalTrieNode, PaddedTrieNode};

use crate::traits::{NodeVariant, Node};
use crate::types::Metadata;
use ipnetwork::IpNetwork;
use std::sync::Arc;

//
// NODE WRAPPER ENUM
// Provides a single concrete type that implements `Node` for all variants.
// Trees store `Arc<dyn Node>` — the NodeWrapper is the concrete type
// that gets erased behind that pointer.
//

pub enum NodeWrapper {
    // Uncompressed variants
    NormalTrieNode(Arc<NormalTrieNode>),
    AtomicTrieNode(Arc<AtomicTrieNode>),
    PaddedTrieNode(Arc<PaddedTrieNode>),
    LockFreeTrieNode(Arc<LockFreeTrieNode>),
    // Compressed (Patricia) variants
    NormalRadixNode(Arc<NormalRadixNode>),
    AtomicRadixNode(Arc<AtomicRadixNode>),
    PaddedRadixNode(Arc<PaddedRadixNode>),
    LockFreeRadixNode(Arc<LockFreeRadixNode>),
}

/// Macro to dispatch all Node methods across all variants.
macro_rules! dispatch {
    ($self:ident, $method:ident $(, $arg:expr)*) => {
        match $self {
            NodeWrapper::NormalTrieNode(n)            => n.$method($($arg),*),
            NodeWrapper::AtomicTrieNode(n)            => n.$method($($arg),*),
            NodeWrapper::PaddedTrieNode(n)            => n.$method($($arg),*),
            NodeWrapper::LockFreeTrieNode(n)          => n.$method($($arg),*),
            NodeWrapper::NormalRadixNode(n)  => n.$method($($arg),*),
            NodeWrapper::AtomicRadixNode(n)  => n.$method($($arg),*),
            NodeWrapper::PaddedRadixNode(n)  => n.$method($($arg),*),
            NodeWrapper::LockFreeRadixNode(n)=> n.$method($($arg),*),
        }
    };
}

impl Node for NodeWrapper {
    fn bit(&self) -> Option<u8> {
        dispatch!(self, bit)
    }

    fn left(&self) -> Option<Arc<dyn Node>> {
        dispatch!(self, left)
    }

    fn right(&self) -> Option<Arc<dyn Node>> {
        dispatch!(self, right)
    }

    fn metadata(&self) -> Option<Metadata> {
        dispatch!(self, metadata)
    }

    fn prefix(&self) -> Option<IpNetwork> {
        dispatch!(self, prefix)
    }

    fn set_left(&self, node: Option<Arc<dyn Node>>) {
        dispatch!(self, set_left, node)
    }

    fn set_right(&self, node: Option<Arc<dyn Node>>) {
        dispatch!(self, set_right, node)
    }

    fn set_metadata(&self, metadata: Metadata) {
        dispatch!(self, set_metadata, metadata)
    }

    fn clear_metadata(&self) {
        dispatch!(self, clear_metadata)
    }

    fn set_bit(&self, bit: u8) {
        dispatch!(self, set_bit, bit)
    }

    fn set_prefix(&self, prefix: IpNetwork) {
        dispatch!(self, set_prefix, prefix)
    }

    fn edge_bits(&self) -> Option<[u8; 16]> {
        dispatch!(self, edge_bits)
    }

    fn edge_len(&self) -> Option<usize> {
        dispatch!(self, edge_len)
    }

    fn set_edge(&self, bits: [u8; 16], len: usize) {
        dispatch!(self, set_edge, bits, len)
    }
}

//
// NODE BUILDER
// Factory that creates the right NodeWrapper variant from a NodeVariant enum.
//

#[derive(Clone)]
pub struct NodeBuilder {
    variant: NodeVariant,
}

impl NodeBuilder {
    pub fn new(variant: NodeVariant) -> Self {
        Self { variant }
    }

    /// Build an empty node of the configured variant.
    pub fn build(&self) -> Arc<dyn Node> {
        match self.variant {
            NodeVariant::NormalTrieNode => Arc::new(NodeWrapper::NormalTrieNode(Arc::new(NormalTrieNode::new()))),
            NodeVariant::AtomicTrieNode => Arc::new(NodeWrapper::AtomicTrieNode(Arc::new(AtomicTrieNode::new()))),
            NodeVariant::PaddedTrieNode => Arc::new(NodeWrapper::PaddedTrieNode(Arc::new(PaddedTrieNode::new()))),
            NodeVariant::LockFreeTrieNode => Arc::new(NodeWrapper::LockFreeTrieNode(Arc::new(LockFreeTrieNode::new()))),
            NodeVariant::NormalRadixNode => Arc::new(NodeWrapper::NormalRadixNode(Arc::new(
                NormalRadixNode::new(),
            ))),
            NodeVariant::AtomicRadixNode => Arc::new(NodeWrapper::AtomicRadixNode(Arc::new(
                AtomicRadixNode::new(),
            ))),
            NodeVariant::PaddedRadixNode => Arc::new(NodeWrapper::PaddedRadixNode(Arc::new(
                PaddedRadixNode::new(),
            ))),
            NodeVariant::LockFreeRadixNode => Arc::new(NodeWrapper::LockFreeRadixNode(Arc::new(
                LockFreeRadixNode::new(),
            ))),
        }
    }

    /// Build a leaf node (a node that immediately stores a prefix + metadata).
    pub fn build_leaf(&self, network: IpNetwork, metadata: Metadata) -> Arc<dyn Node> {
        let node = self.build();
        node.set_prefix(network);
        node.set_metadata(metadata);
        node
    }
}
