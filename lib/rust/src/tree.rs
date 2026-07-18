use std::net::IpAddr;
use std::sync::Arc;
use ipnetwork::IpNetwork;
use crate::lpm::{get_bit, longest_prefix_match_binary};
use crate::node::NodeBuilder;
use crate::traits::{NodeVariant, RadixNode, RouteTree};
use crate::types::Metadata;

#[derive(Clone)]
pub struct UncompressedTree {
    root: Arc<dyn RadixNode>,
    node_builder: NodeBuilder,
}

impl UncompressedTree {
    pub fn new(node_variant: NodeVariant) -> Self {
        let builder = NodeBuilder::new(node_variant);
        Self {
            root: builder.build(),
            node_builder: builder,
        }
    }
}

impl RouteTree for UncompressedTree {
    fn insert(&self, prefix: IpNetwork, metadata: Metadata) -> Result<bool, String> {
        let ip = prefix.network();
        let prefix_len = prefix.prefix() as usize;

        let mut current_arc = self.root.clone();
        
        for depth in 0..prefix_len {
            let bit = get_bit(ip, depth);
            let next = if bit == 0 {
                current_arc.left()
            } else {
                current_arc.right()
            };

            match next {
                Some(node) => {
                    current_arc = node;
                }
                None => {
                    let new_node = self.node_builder.build();
                    if bit == 0 {
                        current_arc.set_left(Some(new_node.clone()));
                    } else {
                        current_arc.set_right(Some(new_node.clone()));
                    }
                    current_arc = new_node;
                }
            }
        }
        
        let is_new = current_arc.metadata().is_none();
        current_arc.set_prefix(prefix);
        current_arc.set_metadata(metadata);

        Ok(is_new)
    }

    fn lookup(&self, ip: &IpAddr) -> Option<Metadata> {
        longest_prefix_match_binary(&*self.root, *ip)
    }

    fn remove(&self, prefix: &IpNetwork) -> Option<Metadata> {
        let ip = prefix.network();
        let prefix_len = prefix.prefix() as usize;

        let mut current_arc = self.root.clone();
        
        for depth in 0..prefix_len {
            let bit = get_bit(ip, depth);
            let next = if bit == 0 {
                current_arc.left()
            } else {
                current_arc.right()
            };
            match next {
                Some(node) => current_arc = node,
                None => return None,
            }
        }

        let removed = current_arc.metadata();
        if removed.is_some() {
            current_arc.clear_metadata();
        }

        removed
    }

    fn contains(&self, prefix: &IpNetwork) -> bool {
        let ip = prefix.network();
        let prefix_len = prefix.prefix() as usize;

        let mut current_arc = self.root.clone();
        
        for depth in 0..prefix_len {
            let bit = get_bit(ip, depth);
            let next = if bit == 0 {
                current_arc.left()
            } else {
                current_arc.right()
            };
            match next {
                Some(node) => current_arc = node,
                None => return false,
            }
        }

        current_arc.metadata().is_some()
    }

    fn clear(&self) {
        self.root.set_left(None);
        self.root.set_right(None);
    }
}
