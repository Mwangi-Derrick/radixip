use crate::token_bucket::TokenBucketLimiter;
use std::collections::HashMap;

pub struct RouteTrie {
    root: RouteTrieNode,
}

struct RouteTrieNode {
    pattern: String,
    token_bucket: TokenBucketLimiter,
    children: HashMap<String, RouteTrieNode>,
}

impl RouteTrieNode {
    pub fn new() -> Self {
        Self {
            pattern: String::new(),
            token_bucket: TokenBucketLimiter::new(),
            children: HashMap::new(),
        }
    }
}

impl RouteTrie {
    pub fn new() -> Self {
        Self {
            root: RouteTrieNode::new(),
        }
    }

    pub fn insert(&mut self, route: &str) {
        let mut node = &mut self.root;
        for segment in route.split('/') {
            node = node
                .children
                .entry(segment.to_string())
                .or_insert_with(RouteTrieNode::new);
        }
        node.pattern = route.to_string();
    }

    pub fn match_route(&self, route: &str) -> bool {
        let mut node = &self.root;
        for segment in route.split('/') {
            node = node.children.get(segment).unwrap();
        }
        node.pattern == route
    }
}
