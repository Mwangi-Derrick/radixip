use std::collections::HashMap;

use ipnetwork::IpNetwork;
use serde::{Deserialize, Serialize};

/// User payload stored at a terminal prefix.
///
/// The string map keeps the ABI and Redis payloads simple while still allowing
/// callers to attach labels such as "action", "asn", "country", or "reason".
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
pub struct Metadata {
    pub value: String,
    pub attributes: HashMap<String, String>,
}

impl Metadata {
    pub fn new(value: impl Into<String>) -> Self {
        Self {
            value: value.into(),
            attributes: HashMap::new(),
        }
    }

    pub fn with_attribute(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.attributes.insert(key.into(), value.into());
        self
    }
}

impl From<&str> for Metadata {
    fn from(value: &str) -> Self {
        Self::new(value)
    }
}

impl From<String> for Metadata {
    fn from(value: String) -> Self {
        Self::new(value)
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SubnetRule {
    pub prefix: IpNetwork,
    pub metadata: Metadata,
}

impl SubnetRule {
    pub fn new(prefix: IpNetwork, metadata: Metadata) -> Self {
        Self { prefix, metadata }
    }
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct EngineStats {
    pub inserts: usize,
    pub lookups: usize,
    pub hits: usize,
    pub misses: usize,
    pub removals: usize,
    pub size: usize,
}
