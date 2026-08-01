// tree.rs
use super::{Node, Node4, LeafNode};
use std::net::IpAddr;
use std::ptr;

pub struct Tree {
    root: Box<dyn Node>,
    size: usize,
}

impl Tree {
    pub fn new() -> Self {
        Tree {
            root: Box::new(Node4::default()),
            size: 0,
        }
    }
    
    pub fn insert(&mut self, ip: &[u8], value: *mut ()) {
        self.root = self.insert_node(self.root, ip, 0, value);
        self.size += 1;
    }
    
    fn insert_node(&mut self, node: Box<dyn Node>, key: &[u8], depth: usize, value: *mut ()) -> Box<dyn Node> {
        if depth >= key.len() {
            // Create leaf node
            let leaf = Box::new(LeafNode { value });
            return unsafe { std::mem::transmute(leaf) };
        }
        
        let mut node = node;
        let byte = key[depth];
        
        if let Some(child) = node.find_child(byte) {
            // Recurse into child
            // Need to get the node type from the child pointer
            // This is where it gets complex with dyn traits
            // For now, we'll use a simplified approach
            return node;
        }
        
        // No child, create new leaf
        let leaf = Box::new(LeafNode { value });
        let leaf_ptr = Box::into_raw(leaf) as *mut ();
        
        if node.is_full() {
            node = node.grow();
        }
        
        node = node.add_child(byte, leaf_ptr);
        node
    }
    
    pub fn match_ip(&self, ip: &[u8]) -> Option<*mut ()> {
        self.search(&*self.root, ip, 0)
    }
    
    fn search(&self, node: &dyn Node, key: &[u8], depth: usize) -> Option<*mut ()> {
        if depth >= key.len() {
            // Should be a leaf
            return Some(ptr::null_mut());
        }
        
        let byte = key[depth];
        if let Some(child) = node.find_child(byte) {
            // Need to get the node type from the child
            // This is complex with dyn traits
            // For now, placeholder
            self.search(unsafe { &*(child as *const dyn Node) }, key, depth + 1)
        } else {
            None
        }
    }
}