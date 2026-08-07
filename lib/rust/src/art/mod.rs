// mod.rs
use std::ptr;
pub mod node16_simd;
pub mod tree;
pub use tree::Tree;
#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NodeType {
    Node4 = 0,
    Node16 = 1,
    Node48 = 2,
    Node256 = 3,
    Leaf = 4,
}

#[repr(C)]
#[derive(Debug, Clone, Copy)]
pub struct Header {
    pub node_type: NodeType,
    pub num_children: u8,
    pub prefix_len: u8,
    pub prefix: [u8; 8],
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct Node4 {
    pub header: Header,
    pub keys: [u8; 4],
    pub children: [*mut (); 4],
}

impl Default for Node4 {
    fn default() -> Self {
        Self {
            header: Header {
                node_type: NodeType::Node4,
                num_children: 0,
                prefix_len: 0,
                prefix: [0; 8],
            },
            keys: [0; 4],
            children: [ptr::null_mut(); 4],
        }
    }
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct Node16 {
    pub header: Header,
    pub keys: [u8; 16],
    pub children: [*mut (); 16],
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct Node48 {
    pub header: Header,
    pub index: [u8; 256],
    pub children: [*mut (); 48],
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct Node256 {
    pub header: Header,
    pub children: [*mut (); 256],
}

/// LeafNode stores the value pointer plus prefix metadata so that
/// non-byte-aligned prefixes (/25, /17, etc.) can be matched correctly.
pub struct LeafNode {
    /// Raw pointer to the caller-managed value (e.g. Box<Metadata>).
    pub value: *mut (),
    /// CIDR prefix length (0-32 for IPv4, 0-128 for IPv6).
    pub prefix_len: u8,
    /// Full IP key with host bits zeroed, stored as 16 bytes so both
    /// IPv4 (4 bytes significant) and IPv6 (16 bytes significant) fit.
    pub masked_key: [u8; 16],
}

impl LeafNode {
    /// Returns true when the significant bits of `ip` match this leaf's prefix.
    pub fn matches(&self, ip: &[u8]) -> bool {
        let full_bytes = (self.prefix_len / 8) as usize;
        let remain_bits = (self.prefix_len % 8) as usize;

        // Full bytes must match exactly.
        for i in 0..full_bytes.min(ip.len()) {
            if ip[i] != self.masked_key[i] {
                return false;
            }
        }
        // Boundary byte: check only the significant high bits.
        if remain_bits > 0 && full_bytes < ip.len() {
            let mask = 0xFF_u8 << (8 - remain_bits);
            if ip[full_bytes] & mask != self.masked_key[full_bytes] & mask {
                return false;
            }
        }
        true
    }
}

pub enum NodeBox {
    N4(Node4),
    N16(Node16),
    N48(Node48),
    N256(Node256),
    Leaf(LeafNode),
}

impl NodeBox {
    pub fn node_type(&self) -> NodeType {
        match self {
            NodeBox::N4(_) => NodeType::Node4,
            NodeBox::N16(_) => NodeType::Node16,
            NodeBox::N48(_) => NodeType::Node48,
            NodeBox::N256(_) => NodeType::Node256,
            NodeBox::Leaf(_) => NodeType::Leaf,
        }
    }

    pub fn is_empty(&self) -> bool {
        match self {
            NodeBox::N4(n) => n.header.num_children == 0,
            NodeBox::N16(n) => n.header.num_children == 0,
            NodeBox::N48(n) => n.header.num_children == 0,
            NodeBox::N256(n) => n.header.num_children == 0,
            NodeBox::Leaf(_) => false,
        }
    }

    pub fn is_full(&self) -> bool {
        match self {
            NodeBox::N4(n) => n.header.num_children >= 4,
            NodeBox::N16(n) => n.header.num_children >= 16,
            NodeBox::N48(n) => n.header.num_children >= 48,
            NodeBox::N256(n) => n.header.num_children >= 255,
            NodeBox::Leaf(_) => true,
        }
    }

    pub fn max_children(&self) -> u8 {
        match self {
            NodeBox::N4(_) => 4,
            NodeBox::N16(_) => 16,
            NodeBox::N48(_) => 48,
            NodeBox::N256(_) => 256,
            NodeBox::Leaf(_) => 0,
        }
    }

    pub fn min_children(&self) -> u8 {
        match self {
            NodeBox::N4(_) => 1,
            NodeBox::N16(_) => 4,
            NodeBox::N48(_) => 17,
            NodeBox::N256(_) => 49,
            NodeBox::Leaf(_) => 0,
        }
    }

    pub fn find_child(&self, byte: u8) -> Option<*mut ()> {
        match self {
            NodeBox::N4(n) => {
                for i in 0..(n.header.num_children as usize) {
                    if n.keys[i] == byte {
                        return Some(n.children[i]);
                    }
                }
                None
            }
            NodeBox::N16(n) => {
                let count = n.header.num_children;
                if let Some(idx) = node16_simd::simd_find_child(&n.keys, byte, count) {
                    return Some(n.children[idx]);
                }
                None
            }
            NodeBox::N48(n) => {
                let idx = n.index[byte as usize];
                if idx < 48 {
                    Some(n.children[idx as usize])
                } else {
                    None
                }
            }
            NodeBox::N256(n) => {
                let child = n.children[byte as usize];
                if !child.is_null() { Some(child) } else { None }
            }
            NodeBox::Leaf(_) => None,
        }
    }

    // returns a boxed NodeBox (possibly different variant)
    pub fn add_child(&mut self, byte: u8, child: *mut ()) -> Box<NodeBox> {
        match self {
            NodeBox::N4(n) => {
                if n.header.num_children >= 4 {
                    let mut new = Node16 {
                        header: n.header,
                        keys: [0; 16],
                        children: [ptr::null_mut(); 16],
                    };
                    for i in 0..(n.header.num_children as usize) {
                        new.keys[i] = n.keys[i];
                        new.children[i] = n.children[i];
                    }
                    new.header.node_type = NodeType::Node16;
                    let mut nb = NodeBox::N16(new);
                    return nb.add_child(byte, child);
                }
                let idx = n.header.num_children as usize;
                n.keys[idx] = byte;
                n.children[idx] = child;
                n.header.num_children += 1;
                Box::new(NodeBox::N4(*n))
            }
            NodeBox::N16(n) => {
                if n.header.num_children >= 16 {
                    // grow to 48
                    let mut new = Node48 {
                        header: n.header,
                        index: [48; 256],
                        children: [ptr::null_mut(); 48],
                    };
                    for i in 0..(n.header.num_children as usize) {
                        let b = n.keys[i];
                        new.index[b as usize] = i as u8;
                        new.children[i] = n.children[i];
                    }
                    new.header.node_type = NodeType::Node48;
                    let mut nb = NodeBox::N48(new);
                    return nb.add_child(byte, child);
                }
                let idx = n.header.num_children as usize;
                n.keys[idx] = byte;
                n.children[idx] = child;
                n.header.num_children += 1;
                Box::new(NodeBox::N16(*n))
            }
            NodeBox::N48(n) => {
                if n.header.num_children >= 48 {
                    // grow to 256
                    let mut new = Node256 {
                        header: n.header,
                        children: [ptr::null_mut(); 256],
                    };
                    for i in 0..256usize {
                        let idx = n.index[i];
                        if (idx as usize) < 48 {
                            new.children[i] = n.children[idx as usize];
                        }
                    }
                    new.header.node_type = NodeType::Node256;
                    let mut nb = NodeBox::N256(new);
                    return nb.add_child(byte, child);
                }
                let idx = n.header.num_children as usize;
                n.index[byte as usize] = idx as u8;
                n.children[idx] = child;
                n.header.num_children += 1;
                if n.header.num_children >= 45 {
                    let mut new = Node256 {
                        header: n.header,
                        children: [ptr::null_mut(); 256],
                    };
                    for i in 0..256usize {
                        let idx = n.index[i];
                        if (idx as usize) < 48 {
                            new.children[i] = n.children[idx as usize];
                        }
                    }
                    new.header.node_type = NodeType::Node256;
                    return Box::new(NodeBox::N256(new));
                }
                Box::new(NodeBox::N48(*n))
            }
            NodeBox::N256(n) => {
                if n.children[byte as usize].is_null() {
                    n.header.num_children += 1;
                }
                n.children[byte as usize] = child;
                Box::new(NodeBox::N256(*n))
            }
            NodeBox::Leaf(_) => Box::new(NodeBox::Leaf(LeafNode {
                value: child,
                prefix_len: self.header.prefix_len,
                masked_key: self.header.masked_key,
            })),
        }
    }

    pub fn remove_child(&mut self, byte: u8) -> Box<NodeBox> {
        match self {
            NodeBox::N4(n) => {
                for i in 0..(n.header.num_children as usize) {
                    if n.keys[i] == byte {
                        for j in i..(n.header.num_children as usize - 1) {
                            n.keys[j] = n.keys[j + 1];
                            n.children[j] = n.children[j + 1];
                        }
                        n.header.num_children -= 1;
                        return Box::new(NodeBox::N4(*n));
                    }
                }
                Box::new(NodeBox::N4(*n))
            }
            NodeBox::N16(n) => {
                for i in 0..(n.header.num_children as usize) {
                    if n.keys[i] == byte {
                        for j in i..(n.header.num_children as usize - 1) {
                            n.keys[j] = n.keys[j + 1];
                            n.children[j] = n.children[j + 1];
                        }
                        n.header.num_children -= 1;
                        if n.header.num_children < 4 {
                            let mut new = Node4 {
                                header: n.header,
                                keys: [0; 4],
                                children: [ptr::null_mut(); 4],
                            };
                            for k in 0..(n.header.num_children as usize) {
                                new.keys[k] = n.keys[k];
                                new.children[k] = n.children[k];
                            }
                            new.header.node_type = NodeType::Node4;
                            return Box::new(NodeBox::N4(new));
                        }
                        return Box::new(NodeBox::N16(*n));
                    }
                }
                Box::new(NodeBox::N16(*n))
            }
            NodeBox::N48(n) => {
                let idx = n.index[byte as usize];
                if (idx as usize) < 48 {
                    n.children[idx as usize] = ptr::null_mut();
                    n.index[byte as usize] = 48;
                    n.header.num_children -= 1;
                    if n.header.num_children < 17 {
                        let mut new = Node16 {
                            header: n.header,
                            keys: [0; 16],
                            children: [ptr::null_mut(); 16],
                        };
                        let mut child_idx = 0usize;
                        for i in 0..256usize {
                            let idx = n.index[i];
                            if (idx as usize) < 48 && !n.children[idx as usize].is_null() {
                                new.keys[child_idx] = i as u8;
                                new.children[child_idx] = n.children[idx as usize];
                                child_idx += 1;
                            }
                        }
                        new.header.num_children = child_idx as u8;
                        new.header.node_type = NodeType::Node16;
                        return Box::new(NodeBox::N16(new));
                    }
                    return Box::new(NodeBox::N48(*n));
                }
                Box::new(NodeBox::N48(*n))
            }
            NodeBox::N256(n) => {
                if !n.children[byte as usize].is_null() {
                    n.children[byte as usize] = ptr::null_mut();
                    n.header.num_children -= 1;
                    if n.header.num_children < 49 {
                        let mut new = Node48 {
                            header: n.header,
                            index: [48; 256],
                            children: [ptr::null_mut(); 48],
                        };
                        for i in 0..256usize {
                            new.index[i] = 48;
                        }
                        let mut child_idx: u8 = 0;
                        for i in 0..256usize {
                            if !n.children[i].is_null() {
                                new.index[i] = child_idx;
                                new.children[child_idx as usize] = n.children[i];
                                child_idx += 1;
                            }
                        }
                        new.header.node_type = NodeType::Node48;
                        new.header.num_children = child_idx;
                        return Box::new(NodeBox::N48(new));
                    }
                }
                Box::new(NodeBox::N256(*n))
            }
            NodeBox::Leaf(_) => Box::new(NodeBox::Leaf(LeafNode {
                value: ptr::null_mut(),
                prefix_len: 0,
                masked_key: 0,
            })),
        }
    }
}
