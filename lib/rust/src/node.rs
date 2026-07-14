use std::net::IpAddr;
use std::sync::Arc;
use ipnetwork::IpNetwork;
use std::sync::atomic::{AtomicPtr, Ordering};
use std::collections::HashMap;
use crate::types::Metadata;

/// Binary radix tree node (cache-line aligned)
#[repr(C, align(64))]  // 64-byte cache line alignment
pub struct RadixNode {
    pub bit:  Option<u8>,
    pub left: Option<Arc<RadixNode>>,
    pub right: Option<Arc<RadixNode>>,
    pub metadata: Option<Metadata>,
    pub prefix: Option<IpNetwork>,//represents the ip network range of a subnet
    pub children: HashMap<IpNetwork, Arc<RadixNode>>,
}

impl RadixNode {
    pub fn new() -> Self {
        Self {
            bit: None,
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

   pub fn get_left(&self) -> *mut RadixNode {
        self.left.load(Ordering::Acquire)
    }

    // SetLeft safely sets the left child
    pub fn set_left(&self, child: *mut RadixNode) {
        self.left.store(child, Ordering::Release);
    }

    pub fn get_prefix(&self) -> *mut IpNetwork {
        return self.prefix.load(Ordering::Acquire);
    }

    pub fn set_prefix(&self, prefix: *mut IpNetwork) {
        self.prefix.store(prefix, Ordering::Release);
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