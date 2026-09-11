use ipnetwork;
use napi::bindgen_prelude::*;
use napi_derive::napi;
use radixip::{Metadata, RadixEngine}; // Note: no UncompressedTree import needed for wrapper approach
use radixip_config::RadixIpConfig;
use radixip_policy::{PackedIp, PolicyDecisionCode, PolicyHandle};
use std::collections::HashMap;
use std::net::IpAddr;

// Config object passed from JS/TS

#[napi(object)]
pub struct EngineConfig {
    pub variant: Option<String>,
    pub read_compressed: Option<bool>,
    pub write_compressed: Option<bool>,
    pub enable_split_plane: Option<bool>,
    pub cache_enabled: Option<bool>,
    pub cache_max_entries: Option<u32>,
    pub cache_ttl_seconds: Option<u32>,
    pub redis_url: Option<String>,
    pub redis_channel: Option<String>,
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

#[napi(object)]
pub struct JsPolicyResult {
    pub decision: String,
    pub retry_after_seconds: u32,
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
pub struct RadixPolicy {
    inner: PolicyHandle,
}

#[napi]
impl RadixPolicy {
    #[napi(factory)]
    pub fn from_yaml(path: String) -> napi::Result<Self> {
        let config = RadixIpConfig::from_file(&path)
            .map_err(|e| Error::new(Status::InvalidArg, e.to_string()))?;
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .map_err(|e| Error::new(Status::GenericFailure, e.to_string()))?;
        let engine = std::sync::Arc::new(runtime.block_on(radixip::new_balanced()));
        Ok(Self {
            inner: PolicyHandle::new(engine, &config),
        })
    }

    #[napi]
    pub fn check_ip(&self, ip: String) -> napi::Result<JsPolicyResult> {
        let addr = ip
            .parse::<IpAddr>()
            .map_err(|_| Error::new(Status::InvalidArg, format!("Invalid IP: {ip}")))?;
        let result = self
            .inner
            .check(PackedIp::from(addr))
            .ok_or_else(|| Error::new(Status::InvalidArg, "Invalid IP address family"))?;
        let decision = match result.decision {
            PolicyDecisionCode::Allow => "allow",
            PolicyDecisionCode::Block => "block",
            PolicyDecisionCode::Limit => "limit",
            PolicyDecisionCode::BadRequest => "bad_request",
        };
        Ok(JsPolicyResult {
            decision: decision.to_owned(),
            retry_after_seconds: result.retry_after_seconds,
        })
    }
}

#[napi]
impl RadixIP {
    #[napi(constructor)]
    pub fn new(config: Option<EngineConfig>) -> Self {
        let mut cfg = radixip::RadixConfig::new();
        if let Some(c) = config {
            if let Some(variant) = c.variant {
                cfg.engine_variant = match variant.as_str() {
                    "standard" => radixip::EngineVariant::Standard,
                    "concurrent" => radixip::EngineVariant::Concurrent,
                    "lockfree" => radixip::EngineVariant::LockFree,
                    "adaptive" => radixip::EngineVariant::Adaptive,
                    "art" => radixip::EngineVariant::ART,
                    _ => cfg.engine_variant,
                };
            }

            cfg.read_compressed = c.read_compressed.unwrap_or(cfg.read_compressed);
            cfg.write_compressed = c.write_compressed.unwrap_or(cfg.write_compressed);
            cfg.enable_split_plane = c.enable_split_plane.unwrap_or(cfg.enable_split_plane);
            cfg.cache_enabled = c.cache_enabled.unwrap_or(cfg.cache_enabled);
            cfg.cache_max_entries = c.cache_max_entries.map(|v| v as usize).unwrap_or(cfg.cache_max_entries);
            cfg.cache_ttl_seconds = c.cache_ttl_seconds.map(|v| v as u64);

            if let Some(redis_url) = c.redis_url {
                cfg.redis = Some(radixip::redis::RedisConfig {
                    url: redis_url,
                    pool_size: 10,
                    connect_timeout: std::time::Duration::from_secs(5),
                    max_retries: 3,
                });
                cfg.redis_channel = c.redis_channel.unwrap_or_else(|| "radixip:updates".to_string());
            }
        }

        let engine = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(radixip::new(cfg));

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
