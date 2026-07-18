use std::net::IpAddr;
use std::sync::Arc;
use ipnetwork::IpNetwork;
use crate::types::{EngineStats, Metadata};

// Configuration for runtime dispatch
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NodeVariant {
    Normal,      // Standard struct with Mutex
    Atomic,      // Atomic pointers
    Padded,      // Cache-line padded
    LockFree,    // Lock-free with DashMap
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

// Core trait that all nodes must implement
pub trait RadixNode: Send + Sync {
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
