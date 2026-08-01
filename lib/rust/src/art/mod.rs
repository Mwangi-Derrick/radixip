// mod.rs
use std::ptr;

#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NodeType {
    Node4 = 0,
    Node16 = 1,
    Node48 = 2,
    Node256 = 3,
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
pub struct Node4 {
    pub header: Header,
    pub keys: [u8; 4],
    pub children: [*mut (); 4],
}

#[repr(C)]
pub struct Node16 {
    pub header: Header,
    pub keys: [u8; 16],
    pub children: [*mut (); 16],
}

#[repr(C)]
pub struct Node48 {
    pub header: Header,
    pub index: [u8; 256],
    pub children: [*mut (); 48],
}

#[repr(C)]
pub struct Node256 {
    pub header: Header,
    pub children: [*mut (); 256],
}

pub struct LeafNode {
    pub value: *mut (),
}

// Node trait
pub trait Node: Send + Sync {
    fn find_child(&self, byte: u8) -> Option<*mut ()>;
    fn add_child(&mut self, byte: u8, child: *mut ()) -> Box<dyn Node>;
    fn remove_child(&mut self, byte: u8) -> Box<dyn Node>;
    fn is_full(&self) -> bool;
    fn is_empty(&self) -> bool;
    fn max_children(&self) -> u8;
    fn min_children(&self) -> u8;
    fn grow(&mut self) -> Box<dyn Node>;
    fn shrink(&mut self) -> Box<dyn Node>;
    fn node_type(&self) -> NodeType;
}