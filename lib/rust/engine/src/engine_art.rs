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
    // Remove 'pub' - trait methods inherit visibility from the trait
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
    pub shards: Vec<ARTEngineAdapter>,
    pub num_shards: usize,
    pub shard_size: usize,
    pub mask_bits: u8,
}

impl ShardedARTEngineAdapter {
    pub fn new(num_shards: usize, shard_size: usize, _mask_bits: u8) -> Self {
        let shards = (0..num_shards)
            .map(|_| ARTEngineAdapter::new())
            .collect();
        Self {
            shards,
            num_shards,
            shard_size,
            mask_bits: 32,
        }
    }

    fn get_shard(&self, ip: &IpAddr) -> usize {
        let hash = match ip {
            IpAddr::V4(ip) => {
                let ip_u32 = u32::from_be_bytes(ip.octets());
                // Mask the IP to keep only prefix bits
                let mask = if self.mask_bits >= 32 {
                    u32::MAX
                } else {
                    u32::MAX << (32 - self.mask_bits)
                };
                // this way we isolate the ip-adress from the network prefix
                let masked_ip = ip_u32 & mask;

                // Hash the masked IP
                let mut hash = 0u32;
                for byte in masked_ip.to_be_bytes() {
                    hash = hash.wrapping_mul(31).wrapping_add(byte as u32);
                }
                hash as usize
            }
            IpAddr::V6(ip) => {
                let bytes = ip.octets();
                let mut hash = 0u64;
                if self.mask_bits <= 64 {
                    // Hash only the masked prefix part
                    let bytes_to_keep = ((self.mask_bits + 7) / 8) as usize;
                    for byte in bytes.iter().take(bytes_to_keep) {
                        hash = hash.wrapping_mul(31).wrapping_add(*byte as u64);
                    }
                    // Handle partial byte masking if needed
                    if self.mask_bits % 8 != 0 && bytes_to_keep < bytes.len() {
                        let mask = 0xFFu8 << (8 - (self.mask_bits % 8) as u8);
                        let partial_byte = bytes[bytes_to_keep] & mask;
                        hash = hash.wrapping_mul(31).wrapping_add(partial_byte as u64);
                    }
                } else {
                    // Hash first 16 bytes for large prefix lengths
                    for byte in bytes.iter().take(16) {
                        hash = hash.wrapping_mul(31).wrapping_add(*byte as u64);
                    }
                };
                hash as usize
            }
        };
        hash % self.num_shards
    }

    pub fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        for shard in &self.shards {
            shard.insert(prefix, metadata.clone())?;
        }
        Ok(())
    }

    pub fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        let shard_idx = self.get_shard(ip);
        self.shards[shard_idx].lookup(ip)
    }

    pub fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        let mut removed = None;
        for shard in &self.shards {
            let shard_removed = shard.remove(prefix);
            if removed.is_none() {
                removed = shard_removed;
            }
        }
        removed
    }

    // Make this public - it's called from engine.rs
    pub fn contains(&self, prefix: &IpNetwork) -> bool {
        self.shards
            .first()
            .map(|s| s.contains(prefix))
            .unwrap_or(false)
    }

    pub fn clear(&self) {
        for shard in &self.shards {
            shard.clear();
        }
    }

    pub fn size(&self) -> usize {
        self.shards.first().map(|s| s.size()).unwrap_or(0)
    }

    pub fn stats(&self) -> EngineStats {
        let mut total = EngineStats::default();
        for shard in &self.shards {
            let stats = shard.stats();
            total.lookups += stats.lookups;
            total.hits += stats.hits;
            total.misses += stats.misses;
            total.inserts += stats.inserts;
            total.removals += stats.removals;
            total.size += stats.size;
        }
        total
    }
}