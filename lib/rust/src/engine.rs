// ! Radix tree engine implementations with different concurrency models

use std::net::IpAddr;
use std::sync::{
    Arc, RwLock,
    atomic::{AtomicUsize, Ordering},
};

use crate::traits::*;
use crate::tree::UncompressedTree;
use crate::types::{EngineStats, Metadata};
use ipnetwork::IpNetwork;

// STANDARD ENGINE ============

pub struct StandardEngine<T: RouteTree> {
    tree: T,
    size: AtomicUsize,
    stats: RwLock<EngineStats>,
}

impl<T: RouteTree> StandardEngine<T> {
    pub fn new(tree: T) -> Self {
        Self {
            tree,
            size: AtomicUsize::new(0),
            stats: RwLock::new(EngineStats::default()),
        }
    }

    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        let is_new = self.tree.insert(prefix, metadata)?;

        if is_new {
            self.size.fetch_add(1, Ordering::Relaxed);
        }

        let mut stats = self.stats.write().unwrap();
        stats.inserts += 1;
        stats.size = self.size();
        Ok(())
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        let result = self.tree.lookup(ip);

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
        let removed = self.tree.remove(prefix);

        if removed.is_some() {
            self.size.fetch_sub(1, Ordering::Relaxed);
            let mut stats = self.stats.write().unwrap();
            stats.removals += 1;
            stats.size = self.size();
        }

        removed
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        self.tree.contains(prefix)
    }

    fn clear(&self) {
        self.tree.clear();
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

// SHARDED ENGINE ============
// throughput = number_shards * throughput per shard
pub struct ShardedEngine<T: RouteTree> {
    pub shards: Vec<Arc<StandardEngine<T>>>,
    pub num_shards: usize,
    pub mask_bits: u8,
}

impl<T: RouteTree + Clone> ShardedEngine<T> {
    fn new(num_shards: usize, tree: T) -> Self {
        let shards = (0..num_shards)
            .map(|_| Arc::new(StandardEngine::new(tree.clone())))
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

impl<T: RouteTree> RadixEngine for StandardEngine<T> {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        StandardEngine::insert(self, prefix, metadata)
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        StandardEngine::lookup(self, ip)
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        StandardEngine::remove(self, prefix)
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        StandardEngine::contains(self, prefix)
    }

    fn clear(&self) {
        StandardEngine::clear(self)
    }

    fn size(&self) -> usize {
        StandardEngine::size(self)
    }

    fn stats(&self) -> EngineStats {
        StandardEngine::stats(self)
    }
}

impl<T: RouteTree + Clone> RadixEngine for ShardedEngine<T> {
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

// ENGINE WRAPPER ============
// allows switch of different engine modes
pub enum EngineWrapper {
    StandardUncompressed(Arc<StandardEngine<UncompressedTree>>),
    ConcurrentUncompressed(Arc<ShardedEngine<UncompressedTree>>),
}

impl EngineWrapper {
    pub fn new(variant: EngineVariant, node_variant: NodeVariant) -> Self {
        let base_tree = UncompressedTree::new(node_variant);

        match variant {
            EngineVariant::Standard => {
                EngineWrapper::StandardUncompressed(Arc::new(StandardEngine::new(base_tree)))
            }
            EngineVariant::Concurrent => {
                // Use 16 shards by default
                // Note: UncompressedTree must be Clone if ShardedEngine uses tree.clone()
                // Let's implement Clone for UncompressedTree in tree.rs soon
                EngineWrapper::ConcurrentUncompressed(Arc::new(ShardedEngine::new(16, base_tree)))
            }
            EngineVariant::LockFree => {
                let lf_tree = UncompressedTree::new(NodeVariant::LockFree);
                EngineWrapper::StandardUncompressed(Arc::new(StandardEngine::new(lf_tree)))
            }
            EngineVariant::Adaptive => {
                // Choose based on system characteristics
                let cpus = std::thread::available_parallelism()
                    .map(|count| count.get())
                    .unwrap_or(1);
                if cpus > 4 {
                    let at_tree = UncompressedTree::new(NodeVariant::Atomic);
                    EngineWrapper::ConcurrentUncompressed(Arc::new(ShardedEngine::new(
                        cpus * 2,
                        at_tree,
                    )))
                } else {
                    let at_tree = UncompressedTree::new(NodeVariant::Atomic);
                    EngineWrapper::StandardUncompressed(Arc::new(StandardEngine::new(at_tree)))
                }
            }
        }
    }
}

impl RadixEngine for EngineWrapper {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.insert(prefix, metadata),
            EngineWrapper::ConcurrentUncompressed(e) => e.insert(prefix, metadata),
        }
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.lookup(ip),
            EngineWrapper::ConcurrentUncompressed(e) => e.lookup(ip),
        }
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.remove(prefix),
            EngineWrapper::ConcurrentUncompressed(e) => e.remove(prefix),
        }
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.contains(prefix),
            EngineWrapper::ConcurrentUncompressed(e) => e.contains(prefix),
        }
    }

    fn clear(&self) {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.clear(),
            EngineWrapper::ConcurrentUncompressed(e) => e.clear(),
        }
    }

    fn size(&self) -> usize {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.size(),
            EngineWrapper::ConcurrentUncompressed(e) => e.size(),
        }
    }

    fn stats(&self) -> EngineStats {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.stats(),
            EngineWrapper::ConcurrentUncompressed(e) => e.stats(),
        }
    }
}
