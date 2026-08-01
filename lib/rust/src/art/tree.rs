// tree.rs
use super::{Node4, NodeBox, LeafNode};
use std::net::IpAddr;
use std::ptr;

pub struct Tree {
    root: *mut NodeBox,
    size: usize,
}

impl Tree {
    pub fn new() -> Self {
        // Start with Node4
        let n4 = Node4::default();
        let boxed = Box::new(NodeBox::N4(n4));
        let ptr = Box::into_raw(boxed);
        Tree { root: ptr, size: 0 }
    }

    pub fn insert(&mut self, ip: &[u8], value: *mut ()) {
        let root = self.root;
        let new_root = self.insert_node(root, ip, 0, value);
        self.root = new_root;
        self.size += 1;
    }

    fn insert_node(&mut self, node_ptr: *mut NodeBox, key: &[u8], depth: usize, value: *mut ()) -> *mut NodeBox {
        if depth >= key.len() {
            // Create leaf node
            let leaf = Box::new(NodeBox::Leaf(LeafNode { value }));
            return Box::into_raw(leaf);
        }

        let byte = key[depth];

        // If node_ptr is null, create a new Node4
        let node_ptr = if node_ptr.is_null() {
            let n4 = Node4::default();
            Box::into_raw(Box::new(NodeBox::N4(n4)))
        } else {
            node_ptr
        };

        // Check for existing child
        let has_child = unsafe { (&*node_ptr).find_child(byte).is_some() };
        if has_child {
            // Recurse into child: find pointer, then call insert_node on it
            let child_ptr = unsafe { (&*node_ptr).find_child(byte).unwrap() };
            let new_child = self.insert_node(child_ptr as *mut NodeBox, key, depth + 1, value);

            // Replace child pointer in parent with new_child
            // Take ownership of parent box, update, and return new ptr
            let mut parent = unsafe { Box::from_raw(node_ptr) };
            match &mut *parent {
                NodeBox::N4(n) => {
                    for i in 0..(n.header.num_children as usize) {
                        if n.keys[i] == byte { n.children[i] = new_child as *mut (); break; }
                    }
                }
                NodeBox::N16(n) => {
                    if let Some(idx) = super::node16_simd::simd_find_child(&n.keys, byte, n.header.num_children) {
                        n.children[idx] = new_child as *mut ();
                    }
                }
                NodeBox::N48(n) => {
                    let idx = n.index[byte as usize];
                    if (idx as usize) < 48 { n.children[idx as usize] = new_child as *mut (); }
                }
                NodeBox::N256(n) => { n.children[byte as usize] = new_child as *mut (); }
                NodeBox::Leaf(_) => {}
            }
            return Box::into_raw(parent);
        }

        // No child, create new leaf and add
        let leaf = Box::into_raw(Box::new(NodeBox::Leaf(LeafNode { value })));

        // Take ownership of node, add child, drop old, return new
        let mut owned = unsafe { Box::from_raw(node_ptr) };
        let new_box = owned.add_child(byte, leaf as *mut ());
        // owned is dropped here
        Box::into_raw(new_box)
    }

    pub fn match_ip(&self, ip: &[u8]) -> Option<*mut ()> {
        self.search(self.root, ip, 0)
    }

    fn search(&self, node_ptr: *mut NodeBox, key: &[u8], depth: usize) -> Option<*mut ()> {
        if node_ptr.is_null() {
            return None;
        }
        if depth >= key.len() {
            // Should be a leaf
            let node = unsafe { &*node_ptr };
            if let NodeBox::Leaf(leaf) = node { return Some(leaf.value); }
            return None;
        }

        let byte = key[depth];
        let node = unsafe { &*node_ptr };
        if let Some(child_ptr) = node.find_child(byte) {
            self.search(child_ptr as *mut NodeBox, key, depth + 1)
        } else {
            None
        }
    }

    pub fn delete(&mut self, ip: &[u8]) -> bool {
        let mut deleted = false;
        let root = self.delete_node(self.root, ip, 0, &mut deleted);
        if deleted { self.size -= 1; }
        self.root = root;
        deleted
    }

    fn delete_node(&mut self, node_ptr: *mut NodeBox, key: &[u8], depth: usize, deleted: &mut bool) -> *mut NodeBox {
        if node_ptr.is_null() { return ptr::null_mut(); }
        if depth >= key.len() {
            *deleted = true;
            return ptr::null_mut();
        }

        // Take ownership
        let mut owned = unsafe { Box::from_raw(node_ptr) };
        let byte = key[depth];
        if let Some(child_ptr) = owned.find_child(byte) {
            let new_child = self.delete_node(child_ptr as *mut NodeBox, key, depth + 1, deleted);
            if *deleted {
                // Remove child from node
                let new_box = owned.remove_child(byte);
                return Box::into_raw(new_box);
            }
        }
        // No change
        Box::into_raw(owned)
    }

    pub fn Size(&self) -> usize { self.size }
}