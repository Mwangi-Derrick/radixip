use std::net::IpAddr;
use std::sync::Arc;
use ipnetwork::IpNetwork;
use crate::types::Metadata;

/// Binary radix tree node (cache-line aligned)
#[repr(C, align(64))]  // 64-byte cache line alignment
pub struct RadixNode {
    pub left: Option<Arc<RadixNode>>,
    pub right: Option<Arc<RadixNode>>,
    pub metadata: Option<Metadata>,
    pub prefix: Option<IpNetwork>,
    pub children: HashMap<IpNetwork, Arc<RadixNode>>,
}

impl RadixNode {
    pub fn new() -> Self {
        Self {
            left: None,
            right: None,
            metadata: None,
            prefix: None,
        }
    }

    pub fn clone_with_insert(&self, network: IpNetwork, metadata: Metadata) -> Self {
        // Clone this node with the new subnet inserted
        // This is the copy-on-write pattern
        let mut new_node = self.clone();
        // ... insert logic ...
        new_node
    }
}

impl Clone for RadixNode {
    fn clone(&self) -> Self {
        Self {
            left: self.left.clone(),
            right: self.right.clone(),
            metadata: self.metadata.clone(),
            prefix: self.prefix.clone(),
        }
    }
}