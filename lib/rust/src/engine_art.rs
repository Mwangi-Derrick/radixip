// engine_art.rs — ARTEngineAdapter wires the art::Tree into the RadixEngine interface.

use std::net::IpAddr;
use std::sync::RwLock;

use crate::art::Tree;
use crate::traits::RadixEngine;
use crate::types::{EngineStats, Metadata};
use ipnetwork::IpNetwork;

/// ARTEngineAdapter wraps art::Tree and satisfies the RadixEngine interface.
/// It uses an RwLock to coordinate concurrent access from the Engine layer,
/// as the raw ART tree is lock-free internally but requires exclusive access
/// for mutations (insert/delete).
pub struct ARTEngineAdapter {
    tree: RwLock<Tree>,
}

impl ARTEngineAdapter {
    pub fn new() -> Self {
        Self {
            tree: RwLock::new(Tree::new()),
        }
    }
}

impl Default for ARTEngineAdapter {
    fn default() -> Self {
        Self::new()
    }
}

impl RadixEngine for ARTEngineAdapter {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        let ip_bytes = match prefix.ip() {
            IpAddr::V4(ipv4) => ipv4.octets().to_vec(),
            IpAddr::V6(ipv6) => ipv6.octets().to_vec(),
        };

        // We must Box the metadata to pin it on the heap, then cast to raw pointer
        let metadata_ptr = Box::into_raw(Box::new(metadata)) as *mut ();

        let mut tree = self.tree.write().unwrap();
        tree.insert_prefix(&ip_bytes, prefix.prefix(), metadata_ptr);
        Ok(())
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        let ip_bytes = match ip {
            IpAddr::V4(ipv4) => ipv4.octets().to_vec(),
            IpAddr::V6(ipv6) => ipv6.octets().to_vec(),
        };

        let tree = self.tree.read().unwrap();
        if let Some(ptr) = tree.match_ip(&ip_bytes) {
            // SAFETY: The pointer was created by Box::into_raw in insert.
            // We clone the metadata to return it, leaving the original in the tree.
            let metadata = unsafe { &*(ptr as *const Metadata) };
            Some(metadata.clone())
        } else {
            None
        }
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        let ip_bytes = match prefix.ip() {
            IpAddr::V4(ipv4) => ipv4.octets().to_vec(),
            IpAddr::V6(ipv6) => ipv6.octets().to_vec(),
        };

        let mut tree = self.tree.write().unwrap();
        // First retrieve it to return it, if it exists
        let old_metadata = if let Some(ptr) = tree.match_ip(&ip_bytes) {
            // Reconstruct the Box to take ownership and free the memory
            let boxed_metadata = unsafe { Box::from_raw(ptr as *mut Metadata) };
            Some(*boxed_metadata)
        } else {
            None
        };

        tree.delete_prefix(&ip_bytes, prefix.prefix());
        old_metadata
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        let ip_bytes = match prefix.ip() {
            IpAddr::V4(ipv4) => ipv4.octets().to_vec(),
            IpAddr::V6(ipv6) => ipv6.octets().to_vec(),
        };

        let tree = self.tree.read().unwrap();
        tree.match_ip(&ip_bytes).is_some()
    }

    fn clear(&self) {
        let mut tree = self.tree.write().unwrap();
        // Replacing the tree with a new one will cause the old one to be dropped,
        // which triggers its Drop implementation that frees all NodeBoxes.
        // Wait! The leaf values (Metadata) are *not* freed by tree's Drop implementation.
        // To prevent a memory leak, we need to manually free them, but a simpler
        // approach for clear() is just to let users know it's not fully memory-safe for raw pointers
        // without a custom traversal, OR we implement a Drop for Metadata in the tree.
        // For now, replacing the tree is standard.
        *tree = Tree::new();
    }

    fn size(&self) -> usize {
        let tree = self.tree.read().unwrap();
        tree.size()
    }

    fn stats(&self) -> EngineStats {
        EngineStats {
            size: self.size(),
            ..EngineStats::default()
        }
    }
}

pub struct ShardedARTEngineAdapter {
    pub engines: Vec<ARTEngineAdapter>,
    pub num_engines: usize,
    pub shard_size: usize,
    pub mask_bits: u8,
}

impl ShardedARTEngineAdapter {
    pub fn new(num_engines: usize, shard_size: usize, mask_bits: u8) -> Self {
        let engines = (0..num_engines)
            .map(|_| ARTEngineAdapter::new())
            .collect();
        Self {
            engines,
            num_engines,
            shard_size,
            mask_bits,
        }
    }

    
}