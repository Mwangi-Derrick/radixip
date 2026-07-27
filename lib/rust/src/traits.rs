use crate::types::{EngineStats, Metadata};
use ipnetwork::IpNetwork;
use std::matches;
use std::net::IpAddr;
use std::sync::Arc; // Or simply ensure you're using Rust 2018 or later

// Configuration for runtime dispatch
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NodeVariant {
    // Uncompressed (bit-by-bit trie) variants
    Normal,   // Standard struct with RwLock
    Atomic,   // Atomic u8 bit + RwLock children
    Padded,   // Cache-line padded
    LockFree, // Lock-free with DashMap
    // Compressed (Patricia) trie variants
    CompressedNormal,   // Patricia trie with RwLock
    CompressedAtomic,   // Patricia trie with atomic edge encoding
    CompressedPadded,   // Cache-line padded Patricia trie
    CompressedLockFree, // Lock-free Patricia trie with DashMap children
}

impl NodeVariant {
    /// Returns true if this variant uses the compressed Patricia trie.
    pub fn is_compressed(self) -> bool {
        matches!(
            self,
            NodeVariant::CompressedNormal
                | NodeVariant::CompressedAtomic
                | NodeVariant::CompressedPadded
                | NodeVariant::CompressedLockFree
        )
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EngineVariant {
    /// Standard RwLock-based engine
    Standard,
    /// Sharded engine for higher concurrency
    Concurrent,
    /// Fully lock-free engine
    LockFree,
    /// Hybrid engine with adaptive strategies
    Adaptive,
}

// Core trait that all nodes must implement.
//
// Uncompressed nodes leave `edge_bits`, `edge_len`, and `set_edge`
// as the default no-op implementations; only compressed nodes
// provide real implementations for those.
pub trait RadixNode: Send + Sync {
    // ---- Shared methods (both tree types) ----
    fn bit(&self) -> Option<u8>;
    fn left(&self) -> Option<Arc<dyn RadixNode>>;
    fn right(&self) -> Option<Arc<dyn RadixNode>>;
    fn metadata(&self) -> Option<Metadata>;
    fn prefix(&self) -> Option<IpNetwork>;
    fn set_left(&self, node: Option<Arc<dyn RadixNode>>);
    fn set_right(&self, node: Option<Arc<dyn RadixNode>>);
    fn set_metadata(&self, metadata: Metadata);
    fn clear_metadata(&self);
    fn set_bit(&self, bit: u8);
    fn set_prefix(&self, prefix: IpNetwork);

    // ---- Compressed-node extensions (default = uncompressed does nothing) ----

    /// Returns the edge bit-string for Patricia trie nodes.
    /// Uncompressed nodes always return `None`.
    fn edge_bits(&self) -> Option<Vec<u8>> {
        None
    }

    /// Returns the number of valid bits in `edge_bits`.
    /// Uncompressed nodes always return `None`.
    fn edge_len(&self) -> Option<usize> {
        None
    }

    /// Set the edge bit-string and its valid bit length.
    /// Uncompressed nodes panic if called (programming error).
    fn set_edge(&self, _bits: Vec<u8>, _len: usize) {
        panic!("set_edge called on an uncompressed node");
    }
}

// Core engine trait
pub trait RadixEngine: Send + Sync {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String>;
    fn lookup(&self, ip: &IpAddr) -> Option<Metadata>;
    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata>;
    fn contains(&self, prefix: &IpNetwork) -> bool;
    fn clear(&self);
    fn size(&self) -> usize;
    fn stats(&self) -> EngineStats {
        EngineStats {
            size: self.size(),
            ..EngineStats::default()
        }
    }
}

// RouteTree trait to separate routing logic from engine logic
pub trait RouteTree: Send + Sync {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<bool, String>; // returns true if new
    fn lookup(&self, ip: &IpAddr) -> Option<Metadata>;
    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata>;
    fn contains(&self, prefix: &IpNetwork) -> bool;
    fn clear(&self);
}

// Factory trait for creating engines with different variants
pub trait EngineFactory {
    fn create_engine(variant: EngineVariant) -> Box<dyn RadixEngine>;
    fn create_node(variant: NodeVariant) -> Box<dyn RadixNode>;
}
