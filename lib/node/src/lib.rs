use ipnetwork;
use napi::bindgen_prelude::*;
use napi_derive::napi;
use radixip::{Metadata, RadixEngine}; // Note: no UncompressedTree import needed for wrapper approach
use std::collections::HashMap;
use std::net::IpAddr;

// Config object passed from JS/TS

#[napi(object)]
pub struct EngineConfig {
    pub variant: Option<String>,
    pub read_compressed: Option<bool>,
    pub write_compressed: Option<bool>,
    pub enable_split_plane: Option<bool>,
}

// Metadata returned to JS/TS — flat object for ergonomics

#[napi(object)]
pub struct JsMetadata {
    pub value: String,
    pub attributes: HashMap<String, String>,
}

// EngineStats

#[napi(object)]
pub struct JsEngineStats {
    pub size: u32,
    pub inserts: u32,
    pub lookups: u32,
    pub hits: u32,
    pub misses: u32,
    pub removals: u32,
}

// Wrapper for RadixEngine trait object

struct RadixEngineWrapper {
    engine: Box<dyn RadixEngine>,
}

impl RadixEngineWrapper {
    fn new(engine: Box<dyn RadixEngine>) -> Self {
        Self { engine }
    }

    fn insert(
        &self,
        prefix: ipnetwork::IpNetwork,
        metadata: Metadata,
    ) -> std::result::Result<(), String> {
        self.engine
            .insert(prefix, metadata)
            .map_err(|e| e.to_string())
    }

    fn lookup(&self, addr: &IpAddr) -> Option<Metadata> {
        self.engine.lookup(&addr)
    }

    fn remove(&self, prefix: &ipnetwork::IpNetwork) -> Option<Metadata> {
        self.engine.remove(prefix)
    }

    fn clear(&self) {
        self.engine.clear();
    }

    fn size(&self) -> usize {
        self.engine.size()
    }

    fn stats(&self) -> radixip::types::EngineStats {
        self.engine.stats()
    }
}

// RadixIP class

#[napi]
pub struct RadixIP {
    inner: RadixEngineWrapper,
}

#[napi]
impl RadixIP {
    #[napi(constructor)]
    pub fn new(_config: Option<EngineConfig>) -> Self {
        let engine = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(radixip::new_memory_efficient());

        Self {
            inner: RadixEngineWrapper::new(engine),
        }
    }

    #[napi]
    pub fn insert(&self, subnet: String, metadata: JsMetadata) -> napi::Result<()> {
        let Ok(prefix) = subnet.parse::<ipnetwork::IpNetwork>() else {
            return Err(napi::Error::new(
                napi::Status::InvalidArg,
                format!("Invalid CIDR: {subnet}"),
            ));
        };

        let meta = Metadata {
            value: metadata.value,
            attributes: metadata.attributes,
        };

        self.inner
            .insert(prefix, meta)
            .map_err(|e| napi::Error::new(napi::Status::GenericFailure, e))
    }

    #[napi]
    pub fn lookup(&self, ip: String) -> napi::Result<Option<JsMetadata>> {
        let Ok(addr) = ip.parse::<IpAddr>() else {
            return Err(napi::Error::new(
                napi::Status::InvalidArg,
                format!("Invalid IP: {ip}"),
            ));
        };

        Ok(self.inner.lookup(&addr).map(|m| JsMetadata {
            value: m.value,
            attributes: m.attributes,
        }))
    }

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

    #[napi]
    pub fn contains(&self, ip: String) -> napi::Result<bool> {
        let Ok(addr) = ip.parse::<IpAddr>() else {
            return Err(Error::new(Status::InvalidArg, format!("Invalid IP: {ip}")));
        };

        Ok(self.inner.lookup(&addr).is_some())
    }

    #[napi]
    pub fn clear(&self) {
        self.inner.clear();
    }

    #[napi(getter)]
    pub fn size(&self) -> u32 {
        self.inner.size() as u32
    }

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
