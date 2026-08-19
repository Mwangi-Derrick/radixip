//! Radix tree engine implementations with different concurrency models

use std::net::IpAddr;
use std::sync::{
    Arc, RwLock,
    atomic::{AtomicUsize, Ordering},
};
use sysinfo::System;

use crate::traits::*;
use crate::tree::{CompressedTree, UncompressedTree};
use crate::types::{EngineStats, Metadata};
use ipnetwork::IpNetwork;

// ============================================================
// STANDARD ENGINE
// ============================================================

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

// SHARDED ENGINE
// throughput = num_shards * throughput_per_shard

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

// ENGINE WRAPPER
// switches between concurrency models behind one type

#[derive(Clone)]
pub enum EngineWrapper {
    StandardUncompressed(Arc<StandardEngine<UncompressedTree>>),
    ConcurrentUncompressed(Arc<ShardedEngine<UncompressedTree>>),
    StandardCompressed(Arc<StandardEngine<CompressedTree>>),
    ConcurrentCompressed(Arc<ShardedEngine<CompressedTree>>),
    StandardART(Arc<crate::engine_art::ARTEngineAdapter>),
    ConcurrentART(Arc<crate::engine_art::ShardedARTEngineAdapter>),
}

impl EngineWrapper {
    /// `num_shards`:
    ///   `None`      -> auto-detect from CPU cores + available memory
    ///   `Some(1)`   -> standard (unsharded)
    ///   `Some(n>1)` -> sharded with exactly n shards
    pub fn new(
        variant: EngineVariant,
        node_variant: NodeVariant,
        compressed: bool,
        num_shards: Option<usize>,
    ) -> Self {
        let shard_count = num_shards.unwrap_or_else(|| Self::calculate_optimal_shards(&variant));

        // ART carries its own sharding implementation, handled separately.
        if variant == EngineVariant::ART {
            return if shard_count > 1 {
                EngineWrapper::ConcurrentART(Arc::new(
                    crate::engine_art::ShardedARTEngineAdapter::new(shard_count),
                ))
            } else {
                EngineWrapper::StandardART(Arc::new(crate::engine_art::ARTEngineAdapter::new()))
            };
        }

        // LockFree and Adaptive pick their own node representation;
        // everything else uses whatever node_variant the caller passed.
        let resolved_node_variant = match variant {
            EngineVariant::LockFree if compressed => NodeVariant::LockFreeRadixNode,
            EngineVariant::LockFree => NodeVariant::LockFreeTrieNode,
            EngineVariant::Adaptive if compressed => NodeVariant::AtomicRadixNode,
            EngineVariant::Adaptive => NodeVariant::AtomicTrieNode,
            _ => node_variant,
        };

        // LockFree relies on lock-free nodes for its concurrency, not sharding.
        let effective_shards = match variant {
            EngineVariant::LockFree => 1,
            _ => shard_count,
        };

        if compressed {
            let tree = CompressedTree::new(resolved_node_variant);
            if effective_shards > 1 {
                EngineWrapper::ConcurrentCompressed(Arc::new(ShardedEngine::new(
                    effective_shards,
                    tree,
                )))
            } else {
                EngineWrapper::StandardCompressed(Arc::new(StandardEngine::new(tree)))
            }
        } else {
            let tree = UncompressedTree::new(resolved_node_variant);
            if effective_shards > 1 {
                EngineWrapper::ConcurrentUncompressed(Arc::new(ShardedEngine::new(
                    effective_shards,
                    tree,
                )))
            } else {
                EngineWrapper::StandardUncompressed(Arc::new(StandardEngine::new(tree)))
            }
        }
    }

    // shard-count auto-detection 

    /// Cores -> shards, roughly matching:
    /// Dev(2c)->1-2, Small(4c)->4-8, Medium(8c)->16-32, Large(16c)->32-64, Enterprise(32c)->64-128
    fn calculate_optimal_shards(variant: &EngineVariant) -> usize {
        match variant {
            EngineVariant::Standard | EngineVariant::LockFree => 1,
            EngineVariant::ART => Self::shards_from_cpu(&[(2, 2), (4, 4), (8, 8)], 16),
            EngineVariant::Concurrent => {
                Self::shards_from_cpu(&[(2, 2), (4, 8), (8, 16), (16, 32)], 64)
            }
            EngineVariant::Adaptive => {
                let cpu_based = Self::shards_from_cpu(&[(2, 2), (4, 8), (8, 16), (16, 32)], 64);
                let mem_based = Self::shards_from_memory();
                // Take the more conservative bound so we don't over-shard
                // (and thus over-allocate) on memory-constrained boxes.
                cpu_based.min(mem_based)
            }
        }
    }

    /// `breakpoints` is `[(max_cpus, shards), ...]` ascending; `default`
    /// applies once core count exceeds every breakpoint.
    fn shards_from_cpu(breakpoints: &[(usize, usize)], default: usize) -> usize {
        let cpus = std::thread::available_parallelism()
            .map(|c| c.get())
            .unwrap_or(1);
        breakpoints
            .iter()
            .find(|(max_cpus, _)| cpus <= *max_cpus)
            .map(|(_, shards)| *shards)
            .unwrap_or(default)
    }

    fn shards_from_memory() -> usize {
        const MB: u64 = 1024 * 1024;
        let usable_mb = Self::usable_memory_bytes() / MB;
        match usable_mb {
            0..=512 => 2,
            513..=2048 => 8,
            2049..=4096 => 16,
            4097..=8192 => 32,
            _ => 64,
        }
    }

    /// Only budget 75% of currently *available* RAM — leave headroom for
    /// the OS, allocator, and other processes rather than assuming the
    /// engine owns the whole machine.
    fn usable_memory_bytes() -> u64 {
        Self::detect_available_memory().saturating_mul(3) / 4
    }

    /// Cross-platform available memory via `sysinfo` (Linux/macOS/Windows/BSD/...).
    /// Deliberately uses `available_memory`, not `total_memory` — total tells
    /// you nothing about what's actually free to allocate right now, and
    /// `System::new()` + `refresh_memory()` avoids pulling in CPU/process/disk
    /// data we don't need just to answer this one question.
    fn detect_available_memory() -> u64 {
        let mut system = System::new();
        system.refresh_memory();
        system.available_memory()
    }
}

impl RadixEngine for EngineWrapper {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.insert(prefix, metadata),
            EngineWrapper::ConcurrentUncompressed(e) => e.insert(prefix, metadata),
            EngineWrapper::StandardCompressed(e) => e.insert(prefix, metadata),
            EngineWrapper::ConcurrentCompressed(e) => e.insert(prefix, metadata),
            EngineWrapper::StandardART(e) => e.insert(prefix, metadata),
            EngineWrapper::ConcurrentART(e) => e.insert(prefix, metadata),
        }
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.lookup(ip),
            EngineWrapper::ConcurrentUncompressed(e) => e.lookup(ip),
            EngineWrapper::StandardCompressed(e) => e.lookup(ip),
            EngineWrapper::ConcurrentCompressed(e) => e.lookup(ip),
            EngineWrapper::StandardART(e) => e.lookup(ip),
            EngineWrapper::ConcurrentART(e) => e.lookup(ip),
        }
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.remove(prefix),
            EngineWrapper::ConcurrentUncompressed(e) => e.remove(prefix),
            EngineWrapper::StandardCompressed(e) => e.remove(prefix),
            EngineWrapper::ConcurrentCompressed(e) => e.remove(prefix),
            EngineWrapper::StandardART(e) => e.remove(prefix),
            EngineWrapper::ConcurrentART(e) => e.remove(prefix),
        }
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.contains(prefix),
            EngineWrapper::ConcurrentUncompressed(e) => e.contains(prefix),
            EngineWrapper::StandardCompressed(e) => e.contains(prefix),
            EngineWrapper::ConcurrentCompressed(e) => e.contains(prefix),
            EngineWrapper::StandardART(e) => e.contains(prefix),
            EngineWrapper::ConcurrentART(e) => e.contains(prefix),
        }
    }

    fn clear(&self) {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.clear(),
            EngineWrapper::ConcurrentUncompressed(e) => e.clear(),
            EngineWrapper::StandardCompressed(e) => e.clear(),
            EngineWrapper::ConcurrentCompressed(e) => e.clear(),
            EngineWrapper::StandardART(e) => e.clear(),
            EngineWrapper::ConcurrentART(e) => e.clear(),
        }
    }

    fn size(&self) -> usize {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.size(),
            EngineWrapper::ConcurrentUncompressed(e) => e.size(),
            EngineWrapper::StandardCompressed(e) => e.size(),
            EngineWrapper::ConcurrentCompressed(e) => e.size(),
            EngineWrapper::StandardART(e) => e.size(),
            EngineWrapper::ConcurrentART(e) => e.size(),
        }
    }

    fn stats(&self) -> EngineStats {
        match self {
            EngineWrapper::StandardUncompressed(e) => e.stats(),
            EngineWrapper::ConcurrentUncompressed(e) => e.stats(),
            EngineWrapper::StandardCompressed(e) => e.stats(),
            EngineWrapper::ConcurrentCompressed(e) => e.stats(),
            EngineWrapper::StandardART(e) => e.stats(),
            EngineWrapper::ConcurrentART(e) => e.stats(),
        }
    }
}