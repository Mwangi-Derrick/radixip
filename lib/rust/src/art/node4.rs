// node4.rs
use super::{Node4, Header, NodeType};
use std::ptr;

impl Default for Node4 {
    fn default() -> Self {
        Node4 {
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

impl Clone for Node4 {
    fn clone(&self) -> Self {
        *self
    }
}