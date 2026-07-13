use std::sync::atomic::AtomicPtr;
use std::net::IpAddr;
use arc_swap::ArcSwap;
use std::sync::Arc;

use crate::node::RadixNode;
use crate::types::Metadata;
use crate::lpm::longest_prefix_match;

/// Lock-free RadixIP engine
pub struct RadixEngine {
    root: ArcSwap<RadixNode>,
    size: std::sync::atomic::AtomicUsize,
}

impl RadixEngine {
    /// Create a new empty engine
    pub fn new() -> Self {
        Self {
            root: ArcSwap::new(Arc::new(RadixNode::new())),
            size: std::sync::atomic::AtomicUsize::new(0),
        }
    }

    /// Insert a subnet with metadata
    pub fn insert(&self, subnet: &str, metadata: Metadata) -> Result<(), RadixError> {
        // Parse subnet
        let network = subnet.parse::<ipnetwork::IpNetwork>()
            .map_err(|_| RadixError::InvalidSubnet)?;
        
        // Copy-on-write: create new tree with insert
        let new_root = self.root.load().clone_with_insert(network, metadata);
        
        // Atomic swap
        self.root.store(Arc::new(new_root));
        self.size.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        
        Ok(())
    }

    /// Match an IP address against the engine
    pub fn match_ip(&self, ip: &str) -> Option<&Metadata> {
        let ip = ip.parse::<IpAddr>().ok()?;
        let root = self.root.load();
        longest_prefix_match(&root, ip)
    }

    /// Get the number of subnets
    pub fn size(&self) -> usize {
        self.size.load(std::sync::atomic::Ordering::Relaxed)
    }

    /// Clear all subnets
    pub fn clear(&self) {
        self.root.store(Arc::new(RadixNode::new()));
        self.size.store(0, std::sync::atomic::Ordering::Relaxed);
    }
}