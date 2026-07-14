//! Radix tree engine implementations with different concurrency models

use std::net::IpAddr;
use std::sync::{Arc, RwLock, atomic::{AtomicUsize, Ordering}};
use std::collections::HashMap;

use crate::traits::*;
use crate::node::NodeBuilder;
use crate::lpm::longest_prefix_match_entries;
use crate::types::{EngineStats, Metadata};
use ipnetwork::IpNetwork;

// ============ STANDARD ENGINE ============

pub struct StandardEngine {
    root: Arc<dyn RadixNode>,
    entries: RwLock<HashMap<IpNetwork, Metadata>>,
    size: AtomicUsize,
    stats: RwLock<EngineStats>,
    node_builder: NodeBuilder,
}

impl StandardEngine {
    pub fn new(node_variant: NodeVariant) -> Self {
        let builder = NodeBuilder::new(node_variant);
        Self {
            root: builder.build(),
            entries: RwLock::new(HashMap::new()),
            size: AtomicUsize::new(0),
            stats: RwLock::new(EngineStats::default()),
            node_builder: builder,
        }
    }
    
    #[allow(dead_code)]
    fn insert_recursive(
        &self,
        node: &dyn RadixNode,
        network: &IpNetwork,
        metadata: Metadata,
        _bit_pos: usize,
    ) -> Arc<dyn RadixNode> {
        // Recursive insertion with longest prefix matching
        // This is simplified - actual implementation would be more complex
        let child = self.node_builder.build_leaf(*network, metadata);
        node.insert_child(*network, child.clone());
        child
    }
    
    #[allow(dead_code)]
    fn lookup_recursive(
        &self,
        _node: &dyn RadixNode,
        ip: &IpAddr,
        _bit_pos: usize,
    ) -> Option<Metadata> {
        // Recursive lookup with longest prefix matching
        let entries = self.entries.read().unwrap();
        longest_prefix_match_entries(entries.iter(), ip)
    }
}

impl RadixEngine for StandardEngine {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<(), String> {
        let mut entries = self.entries.write().unwrap();
        let is_new = entries.insert(prefix, metadata.clone()).is_none();
        let child = self.node_builder.build_leaf(prefix, metadata);
        self.root.insert_child(prefix, child);

        if is_new {
            self.size.fetch_add(1, Ordering::Relaxed);
        }

        let mut stats = self.stats.write().unwrap();
        stats.inserts += 1;
        stats.size = self.size();
        Ok(())
    }
    
    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        let entries = self.entries.read().unwrap();
        let result = longest_prefix_match_entries(entries.iter(), ip);

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
        let mut entries = self.entries.write().unwrap();
        let removed = entries.remove(prefix);
        self.root.remove_child(prefix);

        if removed.is_some() {
            self.size.fetch_sub(1, Ordering::Relaxed);
            let mut stats = self.stats.write().unwrap();
            stats.removals += 1;
            stats.size = self.size();
        }

        removed
    }
    
    fn contains(&self, prefix: &IpNetwork) -> bool {
        self.entries.read().unwrap().contains_key(prefix)
    }
    
    fn clear(&self) {
        // Reset root
        self.entries.write().unwrap().clear();
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
    shards: Vec<Arc<StandardEngine>>,
    num_shards: usize,
}

impl ShardedEngine {
    pub fn new(num_shards: usize, node_variant: NodeVariant) -> Self {
        let shards = (0..num_shards)
            .map(|_| Arc::new(StandardEngine::new(node_variant)))
            .collect();
        Self { shards, num_shards }
    }
    
    fn get_shard(&self, ip: &IpAddr) -> usize {
        let hash = match ip {
            IpAddr::V4(ip) => ip.to_bits() as usize,
            IpAddr::V6(ip) => {
                let bytes = ip.octets();
                let mut hash = 0u64;
                for byte in bytes.iter().take(8) {
                    hash = hash.wrapping_mul(31).wrapping_add(*byte as u64);
                }
                hash as usize
            }
        };
        hash % self.num_shards
    }
    
    fn get_shard_for_network(&self, network: &IpNetwork) -> usize {
        // Use the network address for sharding
        // each shard can deal with its own data block
        // sharding reduces lock contention instead of waiting for a single thread
        self.get_shard(&network.ip())
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
        self.shards.first().map(|s| s.contains(prefix)).unwrap_or(false)
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
