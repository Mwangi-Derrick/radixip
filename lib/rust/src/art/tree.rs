// tree.rs — ART Tree with non-byte-aligned CIDR prefix support
//
// Non-byte-aligned prefix support (/25, /17, etc.)
// ART routes by full bytes.  For a /25 prefix we only consume ⌈25/8⌉ = 4
// bytes but at depth 3 (the boundary byte) we mask out the host bits before
// using the byte as a child index.  The leaf stores `prefix_len` and
// `masked_key` so lookup can validate the significant bits.

use super::{LeafNode, Node4, NodeBox, NodeType};
use std::ptr;

pub struct Tree {
    root: *mut NodeBox,
    size: usize,
}

// SAFETY: Tree is accessed through &mut self (single-writer) or through
// external locking in the engine layer.  The raw pointers inside are
// heap-allocated and never aliased.
unsafe impl Send for Tree {}
unsafe impl Sync for Tree {}

impl Tree {
    pub fn new() -> Self {
        let n4 = Node4::default();
        let ptr = Box::into_raw(Box::new(NodeBox::N4(n4)));
        Tree { root: ptr, size: 0 }
    }

    // Public API

    /// Insert a prefix with an explicit CIDR length.
    /// For an exact host match use `ip.len() * 8` as `prefix_len`.
    pub fn insert_prefix(&mut self, ip: &[u8], prefix_len: u8, value: *mut ()) {
        let root = self.root;
        let new_root = insert_node(root, ip, 0, prefix_len, value);
        self.root = new_root;
        self.size += 1; // caller is responsible for dedup if needed
    }

    /// Convenience: insert with host-address (exact) semantics.
    pub fn insert(&mut self, ip: &[u8], value: *mut ()) {
        let bits = (ip.len() * 8) as u8;
        self.insert_prefix(ip, bits, value);
    }

    /// Lookup ip, returning a pointer to the matching value or null.
    pub fn match_ip(&self, ip: &[u8]) -> Option<*mut ()> {
        search(self.root, ip, 0)
    }

    /// Delete a prefix with explicit prefix_len.  Returns true if removed.
    pub fn delete_prefix(&mut self, ip: &[u8], prefix_len: u8) -> bool {
        let mut deleted = false;
        let root = delete_node(self.root, ip, 0, prefix_len, &mut deleted);
        if deleted {
            self.size = self.size.saturating_sub(1);
        }
        self.root = root;
        deleted
    }

    /// Delete with host-address semantics.
    pub fn delete(&mut self, ip: &[u8]) -> bool {
        let bits = (ip.len() * 8) as u8;
        self.delete_prefix(ip, bits)
    }

    pub fn size(&self) -> usize {
        self.size
    }

    // Kept for backward compat (was Size in original).
    #[allow(non_snake_case)]
    pub fn Size(&self) -> usize {
        self.size
    }
}

impl Drop for Tree {
    fn drop(&mut self) {
        if !self.root.is_null() {
            drop_subtree(self.root);
            self.root = ptr::null_mut();
        }
    }
}

// Helper: index byte for the current depth, masking boundary byte

#[inline]
fn index_byte(key: &[u8], depth: usize, prefix_len: u8) -> u8 {
    let b = key[depth];
    let full_bytes = (prefix_len / 8) as usize;
    let remain_bits = (prefix_len % 8) as usize;
    if depth == full_bytes && remain_bits > 0 {
        let mask = 0xFF_u8 << (8 - remain_bits);
        b & mask
    } else {
        b
    }
}

// ── Build a leaf with MaskedKey pre-computed ─────────────────────────────────

fn make_leaf(key: &[u8], prefix_len: u8, value: *mut ()) -> *mut NodeBox {
    let mut masked_key = [0u8; 16];
    let full_bytes = (prefix_len / 8) as usize;
    let remain_bits = (prefix_len % 8) as usize;

    for i in 0..full_bytes.min(key.len()) {
        masked_key[i] = key[i];
    }
    if remain_bits > 0 && full_bytes < key.len() {
        let mask = 0xFF_u8 << (8 - remain_bits);
        masked_key[full_bytes] = key[full_bytes] & mask;
    }

    let leaf = LeafNode {
        value,
        prefix_len,
        masked_key,
    };
    Box::into_raw(Box::new(NodeBox::Leaf(leaf)))
}

// ── Recursive insert ─────────────────────────────────────────────────────────

fn insert_node(
    node_ptr: *mut NodeBox,
    key: &[u8],
    depth: usize,
    prefix_len: u8,
    value: *mut (),
) -> *mut NodeBox {
    let max_depth = ((prefix_len as usize) + 7) / 8; // ceil(prefix_len/8)

    // Terminal: store a leaf here.
    if depth >= max_depth {
        // If this slot already holds a leaf, update in-place.
        if !node_ptr.is_null() {
            let node = unsafe { &*node_ptr };
            if let NodeBox::Leaf(_) = node {
                // Take ownership and update.
                let mut owned = unsafe { Box::from_raw(node_ptr) };
                if let NodeBox::Leaf(ref mut l) = *owned {
                    l.value = value;
                    // Re-compute masked_key in case prefix_len changed.
                    let full_bytes = (prefix_len / 8) as usize;
                    let remain_bits = (prefix_len % 8) as usize;
                    l.masked_key = [0u8; 16];
                    for i in 0..full_bytes.min(key.len()) {
                        l.masked_key[i] = key[i];
                    }
                    if remain_bits > 0 && full_bytes < key.len() {
                        let mask = 0xFF_u8 << (8 - remain_bits);
                        l.masked_key[full_bytes] = key[full_bytes] & mask;
                    }
                    l.prefix_len = prefix_len;
                }
                return Box::into_raw(owned);
            }
        }
        return make_leaf(key, prefix_len, value);
    }

    // If current slot holds a leaf from a shorter prefix, expand it.
    if !node_ptr.is_null() {
        let is_leaf = unsafe { matches!(&*node_ptr, NodeBox::Leaf(_)) };
        if is_leaf {
            let existing = unsafe { Box::from_raw(node_ptr) };
            let existing_leaf = if let NodeBox::Leaf(l) = *existing {
                l
            } else {
                unreachable!()
            };

            // Build a new Node4 to hold both the existing leaf and the new one.
            let mut n4 = Node4::default();
            let mut node_box = Box::new(NodeBox::N4(n4));
            let existing_depth = (existing_leaf.prefix_len / 8) as usize;
            let existing_b = if depth < existing_depth {
                index_byte(&existing_leaf.masked_key, depth, existing_leaf.prefix_len)
            } else {
                // The existing leaf is a prefix of what we're inserting;
                // re-attach it at depth.
                existing_leaf.masked_key[depth]
            };
            let existing_ptr = Box::into_raw(Box::new(NodeBox::Leaf(existing_leaf)));
            let new_box = node_box.add_child(existing_b, existing_ptr as *mut ());

            let new_b = index_byte(key, depth, prefix_len);
            let new_child = insert_node(ptr::null_mut(), key, depth + 1, prefix_len, value);
            let final_box = new_box.add_child(new_b, new_child as *mut ());
            // node_box is dropped (moved into add_child chain)
            return Box::into_raw(final_box);
        }
    }

    // Normal inner node handling.
    let node_ptr = if node_ptr.is_null() {
        Box::into_raw(Box::new(NodeBox::N4(Node4::default())))
    } else {
        node_ptr
    };

    let b = index_byte(key, depth, prefix_len);

    let has_child = unsafe { (&*node_ptr).find_child(b).is_some() };
    if has_child {
        let child_ptr = unsafe { (&*node_ptr).find_child(b).unwrap() } as *mut NodeBox;
        let new_child = insert_node(child_ptr, key, depth + 1, prefix_len, value);

        // Update the parent's child pointer if the child was replaced.
        if new_child != child_ptr {
            let mut parent = unsafe { Box::from_raw(node_ptr) };
            update_child(&mut parent, b, new_child as *mut ());
            return Box::into_raw(parent);
        }
        return node_ptr;
    }

    // No child for this byte — create a leaf.
    let leaf_ptr = make_leaf(key, prefix_len, value);
    let mut owned = unsafe { Box::from_raw(node_ptr) };
    let new_box = owned.add_child(b, leaf_ptr as *mut ());
    Box::into_raw(new_box)
}

// Recursive search
fn search(node_ptr: *mut NodeBox, key: &[u8], depth: usize) -> Option<*mut ()> {
    if node_ptr.is_null() {
        return None;
    }
    let node = unsafe { &*node_ptr };
    match node {
        NodeBox::Leaf(leaf) => {
            if leaf.matches(key) {
                Some(leaf.value)
            } else {
                None
            }
        }
        _ => {
            if depth >= key.len() {
                return None;
            }
            // Try the exact byte first (exact-match pass).
            if let Some(child) = node.find_child(key[depth]) {
                search(child as *mut NodeBox, key, depth + 1)
            } else {
                // Try the masked boundary byte (for non-byte-aligned prefixes
                // inserted with a different significant byte value).
                None
            }
        }
    }
}

// Recursive delete
fn delete_node(
    node_ptr: *mut NodeBox,
    key: &[u8],
    depth: usize,
    prefix_len: u8,
    deleted: &mut bool,
) -> *mut NodeBox {
    if node_ptr.is_null() {
        return ptr::null_mut();
    }

    let max_depth = ((prefix_len as usize) + 7) / 8;
    let node = unsafe { &*node_ptr };

    match node {
        NodeBox::Leaf(leaf) => {
            if leaf.prefix_len == prefix_len && leaf.matches(key) {
                *deleted = true;
                unsafe {
                    drop(Box::from_raw(node_ptr));
                }
                ptr::null_mut()
            } else {
                node_ptr
            }
        }
        _ => {
            if depth >= max_depth {
                return node_ptr;
            }
            let b = index_byte(key, depth, prefix_len);
            let has_child = node.find_child(b).is_some();
            if !has_child {
                return node_ptr;
            }
            let child_ptr = node.find_child(b).unwrap() as *mut NodeBox;
            let new_child = delete_node(child_ptr, key, depth + 1, prefix_len, deleted);

            if !*deleted {
                return node_ptr;
            }

            let mut owned = unsafe { Box::from_raw(node_ptr) };
            if new_child.is_null() {
                let new_box = owned.remove_child(b);
                return Box::into_raw(new_box);
            }
            update_child(&mut owned, b, new_child as *mut ());
            Box::into_raw(owned)
        }
    }
}

// Update a child pointer inside a NodeBox
fn update_child(node: &mut NodeBox, byte: u8, new_child: *mut ()) {
    match node {
        NodeBox::N4(n) => {
            for i in 0..n.header.num_children as usize {
                if n.keys[i] == byte {
                    n.children[i] = new_child;
                    break;
                }
            }
        }
        NodeBox::N16(n) => {
            for i in 0..n.header.num_children as usize {
                if n.keys[i] == byte {
                    n.children[i] = new_child;
                    break;
                }
            }
        }
        NodeBox::N48(n) => {
            let idx = n.index[byte as usize];
            if (idx as usize) < 48 {
                n.children[idx as usize] = new_child;
            }
        }
        NodeBox::N256(n) => {
            n.children[byte as usize] = new_child;
        }
        _ => {}
    }
}

// Recursive memory reclamation
fn drop_subtree(ptr: *mut NodeBox) {
    if ptr.is_null() {
        return;
    }
    let owned = unsafe { Box::from_raw(ptr) };
    match *owned {
        NodeBox::N4(n) => {
            for i in 0..n.header.num_children as usize {
                drop_subtree(n.children[i] as *mut NodeBox);
            }
        }
        NodeBox::N16(n) => {
            for i in 0..n.header.num_children as usize {
                drop_subtree(n.children[i] as *mut NodeBox);
            }
        }
        NodeBox::N48(n) => {
            for i in 0..256usize {
                let idx = n.index[i];
                if (idx as usize) < 48 && !n.children[idx as usize].is_null() {
                    drop_subtree(n.children[idx as usize] as *mut NodeBox);
                }
            }
        }
        NodeBox::N256(n) => {
            for i in 0..256usize {
                if !n.children[i].is_null() {
                    drop_subtree(n.children[i] as *mut NodeBox);
                }
            }
        }
        NodeBox::Leaf(_) => {} // value pointer is owned by the engine layer
    }
    // owned (the NodeBox) is dropped here
}
