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
    CompressedAtomicNode, CompressedLockFreeNode, CompressedNormalNode, CompressedPaddedNode,
};
pub use uncompressed::{AtomicNode, LockFreeNode, NormalNode, PaddedNode};

use crate::traits::{NodeVariant, RadixNode};
use crate::types::Metadata;
use ipnetwork::IpNetwork;
use std::sync::Arc;

// ============================================================================
// NODE WRAPPER ENUM
// Provides a single concrete type that implements `RadixNode` for all variants.
// Trees store `Arc<dyn RadixNode>` — the NodeWrapper is the concrete type
// that gets erased behind that pointer.
// ============================================================================

pub enum NodeWrapper {
    // Uncompressed variants
    Normal(Arc<NormalNode>),
    Atomic(Arc<AtomicNode>),
    Padded(Arc<PaddedNode>),
    LockFree(Arc<LockFreeNode>),
    // Compressed (Patricia) variants
    CompressedNormal(Arc<CompressedNormalNode>),
    CompressedAtomic(Arc<CompressedAtomicNode>),
    CompressedPadded(Arc<CompressedPaddedNode>),
    CompressedLockFree(Arc<CompressedLockFreeNode>),
}

/// Macro to dispatch all RadixNode methods across all variants.
macro_rules! dispatch {
    ($self:ident, $method:ident $(, $arg:expr)*) => {
        match $self {
            NodeWrapper::Normal(n)            => n.$method($($arg),*),
            NodeWrapper::Atomic(n)            => n.$method($($arg),*),
            NodeWrapper::Padded(n)            => n.$method($($arg),*),
            NodeWrapper::LockFree(n)          => n.$method($($arg),*),
            NodeWrapper::CompressedNormal(n)  => n.$method($($arg),*),
            NodeWrapper::CompressedAtomic(n)  => n.$method($($arg),*),
            NodeWrapper::CompressedPadded(n)  => n.$method($($arg),*),
            NodeWrapper::CompressedLockFree(n)=> n.$method($($arg),*),
        }
    };
}

impl RadixNode for NodeWrapper {
    fn bit(&self) -> Option<u8> {
        dispatch!(self, bit)
    }

    fn left(&self) -> Option<Arc<dyn RadixNode>> {
        dispatch!(self, left)
    }

    fn right(&self) -> Option<Arc<dyn RadixNode>> {
        dispatch!(self, right)
    }

    fn metadata(&self) -> Option<Metadata> {
        dispatch!(self, metadata)
    }

    fn prefix(&self) -> Option<IpNetwork> {
        dispatch!(self, prefix)
    }

    fn set_left(&self, node: Option<Arc<dyn RadixNode>>) {
        dispatch!(self, set_left, node)
    }

    fn set_right(&self, node: Option<Arc<dyn RadixNode>>) {
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

    fn edge_bits(&self) -> Option<Vec<u8>> {
        dispatch!(self, edge_bits)
    }

    fn edge_len(&self) -> Option<usize> {
        dispatch!(self, edge_len)
    }

    fn set_edge(&self, bits: Vec<u8>, len: usize) {
        dispatch!(self, set_edge, bits, len)
    }
}

// ============================================================================
// NODE BUILDER
// Factory that creates the right NodeWrapper variant from a NodeVariant enum.
// ============================================================================

#[derive(Clone)]
pub struct NodeBuilder {
    variant: NodeVariant,
}

impl NodeBuilder {
    pub fn new(variant: NodeVariant) -> Self {
        Self { variant }
    }

    /// Build an empty node of the configured variant.
    pub fn build(&self) -> Arc<dyn RadixNode> {
        match self.variant {
            NodeVariant::Normal => {
                Arc::new(NodeWrapper::Normal(Arc::new(NormalNode::new())))
            }
            NodeVariant::Atomic => {
                Arc::new(NodeWrapper::Atomic(Arc::new(AtomicNode::new())))
            }
            NodeVariant::Padded => {
                Arc::new(NodeWrapper::Padded(Arc::new(PaddedNode::new())))
            }
            NodeVariant::LockFree => {
                Arc::new(NodeWrapper::LockFree(Arc::new(LockFreeNode::new())))
            }
            NodeVariant::CompressedNormal => {
                Arc::new(NodeWrapper::CompressedNormal(Arc::new(
                    CompressedNormalNode::new(),
                )))
            }
            NodeVariant::CompressedAtomic => {
                Arc::new(NodeWrapper::CompressedAtomic(Arc::new(
                    CompressedAtomicNode::new(),
                )))
            }
            NodeVariant::CompressedPadded => {
                Arc::new(NodeWrapper::CompressedPadded(Arc::new(
                    CompressedPaddedNode::new(),
                )))
            }
            NodeVariant::CompressedLockFree => {
                Arc::new(NodeWrapper::CompressedLockFree(Arc::new(
                    CompressedLockFreeNode::new(),
                )))
            }
        }
    }

    /// Build a leaf node (a node that immediately stores a prefix + metadata).
    pub fn build_leaf(&self, network: IpNetwork, metadata: Metadata) -> Arc<dyn RadixNode> {
        let node = self.build();
        node.set_prefix(network);
        node.set_metadata(metadata);
        node
    }
}
