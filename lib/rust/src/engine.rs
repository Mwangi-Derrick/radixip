//! Radix tree engine implementations with different concurrency models

use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::{
    Arc, RwLock,
    atomic::{AtomicUsize, Ordering},
};

use crate::lpm::{get_bit, longest_prefix_match_binary};
use crate::node::NodeBuilder;
use crate::traits::*;
use crate::types::{EngineStats, Metadata};
use ipnetwork::IpNetwork;

// ============ STANDARD ENGINE ============

pub struct StandardEngine {
    root: Arc<dyn RadixNode>,
    size: AtomicUsize,
    stats: RwLock<EngineStats>,
    node_builder: NodeBuilder,
}

impl StandardEngine {
    pub fn new(node_variant: NodeVariant) -> Self {
        let builder = NodeBuilder::new(node_variant);
        Self {
            root: builder.build(),
            size: AtomicUsize::new(0),
            stats: RwLock::new(EngineStats::default()),
            node_builder: builder,
        }
    }

    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        let ip = prefix.network();
        let prefix_len = prefix.prefix() as usize;

        let mut current_arc = self.root.clone();
        
        for depth in 0..prefix_len {
            let bit = get_bit(ip, depth);
            let next = if bit == 0 {
                current_arc.left()
            } else {
                current_arc.right()
            };

            match next {
                Some(node) => {
                    current_arc = node;
                }
                None => {
                    let new_node = self.node_builder.build();
                    if bit == 0 {
                        current_arc.set_left(Some(new_node.clone()));
                    } else {
                        current_arc.set_right(Some(new_node.clone()));
                    }
                    current_arc = new_node;
                }
            }
        }
        
        let is_new = current_arc.metadata().is_none();
        current_arc.set_prefix(prefix);
        current_arc.set_metadata(metadata);

        if is_new {
            self.size.fetch_add(1, Ordering::Relaxed);
        }

        let mut stats = self.stats.write().unwrap();
        stats.inserts += 1;
        stats.size = self.size();
        Ok(())
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        let result = longest_prefix_match_binary(&*self.root, *ip);

        let mut stats = self.stats.write().unwrap();
        stats.lookups += 1;
        if result.is_some() {
            stats.hits += 1;
        } else {
            stats.misses += 1;
        }

        result
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        let ip = prefix.network();
        let prefix_len = prefix.prefix() as usize;

        let mut current_arc = self.root.clone();
        
        for depth in 0..prefix_len {
            let bit = get_bit(ip, depth);
            let next = if bit == 0 {
                current_arc.left()
            } else {
                current_arc.right()
            };
            match next {
                Some(node) => current_arc = node,
                None => return None,
            }
        }

        let removed = current_arc.metadata();
        if removed.is_some() {
            current_arc.clear_metadata();
            self.size.fetch_sub(1, Ordering::Relaxed);
            let mut stats = self.stats.write().unwrap();
            stats.removals += 1;
            stats.size = self.size();
        }

        removed
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        let ip = prefix.network();
        let prefix_len = prefix.prefix() as usize;

        let mut current_arc = self.root.clone();
        
        for depth in 0..prefix_len {
            let bit = get_bit(ip, depth);
            let next = if bit == 0 {
                current_arc.left()
            } else {
                current_arc.right()
            };
            match next {
                Some(node) => current_arc = node,
                None => return false,
            }
        }

        current_arc.metadata().is_some()
    }

    fn clear(&self) {
        self.root.set_left(None);
        self.root.set_right(None);
        self.size.store(0, Ordering::Relaxed);
        let mut stats = self.stats.write().unwrap();
        stats.size = 0;
    }

    fn size(&self) -> usize {
        self.size.load(Ordering::Relaxed)
    }

    fn stats(&self) -> EngineStats {
        let mut stats = self.stats.read().unwrap().clone();
        stats.size = self.size();
        stats
    }
}

// ============ SHARDED ENGINE ============
//throughput = number_shards * throughput per shard
pub struct ShardedEngine {
    pub shards: Vec<Arc<StandardEngine>>,
    pub num_shards: usize,
    pub mask_bits: u8,
}

impl ShardedEngine {
    fn new(num_shards: usize, node_variant: NodeVariant) -> Self {
        let shards = (0..num_shards)
            .map(|_| Arc::new(StandardEngine::new(node_variant)))
            .collect();
        Self {
            shards,
            num_shards,
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
                let bytes_to_hash = if self.mask_bits <= 64 {
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
}

impl RadixEngine for ShardedEngine {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        for shard in &self.shards {
            shard.insert(prefix, metadata.clone())?;
        }
        Ok(())
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        let shard_idx = self.get_shard(ip);
        self.shards[shard_idx].lookup(ip)
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        let mut removed = None;
        for shard in &self.shards {
            let shard_removed = shard.remove(prefix);
            if removed.is_none() {
                removed = shard_removed;
            }
        }
        removed
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        self.shards
            .first()
            .map(|s| s.contains(prefix))
            .unwrap_or(false)
    }

    fn clear(&self) {
        for shard in &self.shards {
            shard.clear();
        }
    }

    fn size(&self) -> usize {
        self.shards.first().map(|s| s.size()).unwrap_or(0)
    }

    fn stats(&self) -> EngineStats {
        let mut total = EngineStats::default();
        for shard in &self.shards {
            let stats = shard.stats();
            total.lookups += stats.lookups;
            total.hits += stats.hits;
            total.misses += stats.misses;
        }
        if let Some(first) = self.shards.first() {
            let stats = first.stats();
            total.inserts = stats.inserts;
            total.removals = stats.removals;
        }
        total.size = self.size();
        total
    }
}

// ============ ENGINE WRAPPER ============
// allows switch of different engine modes
pub enum EngineWrapper {
    Standard(Arc<StandardEngine>),
    Concurrent(Arc<ShardedEngine>),
}

impl EngineWrapper {
    pub fn new(variant: EngineVariant, node_variant: NodeVariant) -> Self {
        match variant {
            EngineVariant::Standard => {
                EngineWrapper::Standard(Arc::new(StandardEngine::new(node_variant)))
            }
            EngineVariant::Concurrent => {
                // Use 16 shards by default
                EngineWrapper::Concurrent(Arc::new(ShardedEngine::new(16, node_variant)))
            }
            EngineVariant::LockFree => {
                // Use atomic variant with lock-free implementations
                EngineWrapper::Standard(Arc::new(StandardEngine::new(NodeVariant::LockFree)))
            }
            EngineVariant::Adaptive => {
                // Choose based on system characteristics
                let cpus = std::thread::available_parallelism()
                    .map(|count| count.get())
                    .unwrap_or(1);
                if cpus > 4 {
                    EngineWrapper::Concurrent(Arc::new(ShardedEngine::new(
                        cpus * 2,
                        NodeVariant::Atomic,
                    )))
                } else {
                    EngineWrapper::Standard(Arc::new(StandardEngine::new(NodeVariant::Atomic)))
                }
            }
        }
    }
}

impl RadixEngine for EngineWrapper {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        match self {
            EngineWrapper::Standard(e) => e.insert(prefix, metadata),
            EngineWrapper::Concurrent(e) => e.insert(prefix, metadata),
        }
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        match self {
            EngineWrapper::Standard(e) => e.lookup(ip),
            EngineWrapper::Concurrent(e) => e.lookup(ip),
        }
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        match self {
            EngineWrapper::Standard(e) => e.remove(prefix),
            EngineWrapper::Concurrent(e) => e.remove(prefix),
        }
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        match self {
            EngineWrapper::Standard(e) => e.contains(prefix),
            EngineWrapper::Concurrent(e) => e.contains(prefix),
        }
    }

    fn clear(&self) {
        match self {
            EngineWrapper::Standard(e) => e.clear(),
            EngineWrapper::Concurrent(e) => e.clear(),
        }
    }

    fn size(&self) -> usize {
        match self {
            EngineWrapper::Standard(e) => e.size(),
            EngineWrapper::Concurrent(e) => e.size(),
        }
    }

    fn stats(&self) -> EngineStats {
        match self {
            EngineWrapper::Standard(e) => e.stats(),
            EngineWrapper::Concurrent(e) => e.stats(),
        }
    }
}
