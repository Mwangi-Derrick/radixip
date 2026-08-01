// node4.rs
use super::{Node, Node4, Node16, NodeType, Header};
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

impl Node for Node4 {
    fn find_child(&self, byte: u8) -> Option<*mut ()> {
        for i in 0..self.header.num_children as usize {
            if self.keys[i] == byte {
                return Some(self.children[i]);
            }
        }
        None
    }
    
    fn add_child(&mut self, byte: u8, child: *mut ()) -> Box<dyn Node> {
        if self.is_full() {
            let mut new_node = self.grow();
            return new_node.add_child(byte, child);
        }
        
        let idx = self.header.num_children as usize;
        self.keys[idx] = byte;
        self.children[idx] = child;
        self.header.num_children += 1;
        Box::new(self.clone())
    }
    
    fn remove_child(&mut self, byte: u8) -> Box<dyn Node> {
        for i in 0..self.header.num_children as usize {
            if self.keys[i] == byte {
                for j in i..self.header.num_children as usize - 1 {
                    self.keys[j] = self.keys[j + 1];
                    self.children[j] = self.children[j + 1];
                }
                self.header.num_children -= 1;
                return Box::new(self.clone());
            }
        }
        Box::new(self.clone())
    }
    
    fn is_full(&self) -> bool {
        self.header.num_children >= 4
    }
    
    fn is_empty(&self) -> bool {
        self.header.num_children == 0
    }
    
    fn max_children(&self) -> u8 { 4 }
    fn min_children(&self) -> u8 { 1 }
    
    fn grow(&mut self) -> Box<dyn Node> {
        let mut new_node = Node16 {
            header: self.header,
            keys: [0; 16],
            children: [ptr::null_mut(); 16],
        };
        
        for i in 0..self.header.num_children as usize {
            new_node.keys[i] = self.keys[i];
            new_node.children[i] = self.children[i];
        }
        new_node.header.node_type = NodeType::Node16;
        Box::new(new_node)
    }
    
    fn shrink(&mut self) -> Box<dyn Node> {
        Box::new(self.clone())
    }
    
    fn node_type(&self) -> NodeType {
        NodeType::Node4
    }
}

impl Clone for Node4 {
    fn clone(&self) -> Self {
        *self
    }
}