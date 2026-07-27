use ipnetwork;
use napi::bindgen_prelude::*;
use napi::*;
use napi_derive::napi;
use radixip::{Metadata, RadixEngine};
use std::collections::HashMap;
use std::net::IpAddr;
use tokio::*;

// ---------------------------------------------------------------------------
// Config object passed from JS/TS
// ---------------------------------------------------------------------------

#[napi(object)]
pub struct EngineConfig {
    /// "standard" | "concurrent" | "lockfree" | "adaptive"
    pub variant: Option<String>,
    /// Whether to use the compressed Patricia trie for reads.
    pub read_compressed: Option<bool>,
    /// Whether to use the compressed Patricia trie for writes.
    pub write_compressed: Option<bool>,
    /// Enable the split-plane (Hybrid) architecture.
    pub enable_split_plane: Option<bool>,
}

// ---------------------------------------------------------------------------
// Metadata returned to JS/TS — flat object for ergonomics
// ---------------------------------------------------------------------------

#[napi(object)]
pub struct JsMetadata {
    pub value: String,
    pub attributes: HashMap<String, String>,
}

// ---------------------------------------------------------------------------
// EngineStats
// ---------------------------------------------------------------------------

#[napi(object)]
pub struct JsEngineStats {
    pub size: u32,
    pub inserts: u32,
    pub lookups: u32,
    pub hits: u32,
    pub misses: u32,
    pub removals: u32,
}

// ---------------------------------------------------------------------------
// RadixIP class
// ---------------------------------------------------------------------------

#[napi]
pub struct RadixIP {
    inner: Box<dyn RadixEngine>,
}

#[napi]
impl RadixIP {
    /// Create a new RadixIP engine.
    /// Pass an optional `EngineConfig` to customise the variant.
    #[napi(constructor)]
    pub fn new(_config: Option<EngineConfig>) -> Self {
        // Node.js is single-threaded; using the Concurrent Sharded engine adds locking overhead
        // with no parallel execution benefits. The memory_efficient config uses the StandardEngine
        // with a compressed Patricia tree, which is optimal for the V8 Event Loop.
        let inner = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(radixip::new_memory_efficient());

        Self { inner }
    }

    /// Insert a CIDR prefix with associated metadata.
    ///
    /// ```ts
    /// engine.insert("10.0.0.0/8", { value: "allow", attributes: { asn: "AS12345" } });
    /// ```
    #[napi]
    pub fn insert(&self, subnet: String, metadata: JsMetadata) -> napi::Result<()> {
        let Ok(prefix) = subnet.parse::<ipnetwork::IpNetwork>() else {
            return Err(Error::new(
                Status::InvalidArg,
                format!("Invalid CIDR: {subnet}"),
            ));
        };
        let meta = Metadata {
            value: metadata.value,
            attributes: metadata.attributes,
        };
        self.inner
            .insert(prefix, meta)
            .map_err(|e| Error::new(Status::GenericFailure, e))
    }

    /// Longest-prefix match. Returns `null` when no prefix covers the IP.
    ///
    /// ```ts
    /// const result = engine.lookup("10.1.2.3");
    /// if (result) console.log(result.value);
    /// ```
    #[napi]
    pub fn lookup(&self, ip: String) -> napi::Result<Option<JsMetadata>> {
        let Ok(addr) = ip.parse::<IpAddr>() else {
            return Err(Error::new(Status::InvalidArg, format!("Invalid IP: {ip}")));
        };
        Ok(self.inner.lookup(&addr).map(|m| JsMetadata {
            value: m.value,
            attributes: m.attributes,
        }))
    }

    /// Remove a prefix from the engine. Returns `true` if the entry was found.
    #[napi]
    pub fn remove(&self, subnet: String) -> napi::Result<bool> {
        let Ok(prefix) = subnet.parse::<ipnetwork::IpNetwork>() else {
            return Err(Error::new(
                Status::InvalidArg,
                format!("Invalid CIDR: {subnet}"),
            ));
        };
        Ok(self.inner.remove(&prefix).is_some())
    }

    /// Returns `true` if any stored prefix covers the given IP.
    #[napi]
    pub fn contains(&self, ip: String) -> napi::Result<bool> {
        let Ok(addr) = ip.parse::<IpAddr>() else {
            return Err(Error::new(Status::InvalidArg, format!("Invalid IP: {ip}")));
        };
        Ok(self.inner.lookup(&addr).is_some())
    }

    /// Remove all entries.
    #[napi]
    pub fn clear(&self) {
        self.inner.clear();
    }

    /// Return the number of stored prefixes.
    #[napi(getter)]
    pub fn size(&self) -> u32 {
        self.inner.size() as u32
    }

    /// Return engine performance statistics.
    #[napi]
    pub fn stats(&self) -> JsEngineStats {
        let s = self.inner.stats();
        JsEngineStats {
            size: s.size as u32,
            inserts: s.inserts as u32,
            lookups: s.lookups as u32,
            hits: s.hits as u32,
            misses: s.misses as u32,
            removals: s.removals as u32,
        }
    }
}

/// Library semantic version.
#[napi]
pub fn version() -> String {
    radixip::VERSION.to_string()
}
