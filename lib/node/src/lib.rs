use napi::bindgen_prelude::*;
use napi_derive::napi;
use radixip_core::RadixEngine;
use radixip_core::Metadata;
use std::collections::HashMap;

#[napi]
pub struct RadixIP {
    inner: RadixEngine,
}

#[napi]
impl RadixIP {
    #[napi(constructor)]
    pub fn new() -> Self {
        Self {
            inner: RadixEngine::new(),
        }
    }

    #[napi]
    pub fn insert(&mut self, subnet: String, metadata: Object) -> Result<()> {
        let meta: HashMap<String, String> = metadata
            .into_serde()
            .map_err(|e| Error::new(Status::InvalidArg, e.to_string()))?;
        
        let meta = Metadata::from(meta);
        self.inner.insert(&subnet, meta)
            .map_err(|e| Error::new(Status::GenericFailure, e.to_string()))?;
        Ok(())
    }

    #[napi]
    pub fn match_ip(&self, ip: String) -> Result<Option<Object>> {
        match self.inner.match_ip(&ip) {
            Some(meta) => {
                let map: HashMap<String, String> = meta.into();
                let obj = Object::from_serde(&map)
                    .map_err(|e| Error::new(Status::GenericFailure, e.to_string()))?;
                Ok(Some(obj))
            }
            None => Ok(None),
        }
    }

    #[napi]
    pub fn size(&self) -> Result<u32> {
        Ok(self.inner.size() as u32)
    }

    #[napi]
    pub fn clear(&mut self) -> Result<()> {
        self.inner.clear();
        Ok(())
    }
}

#[napi]
pub fn version() -> Result<String> {
    Ok(radixip_core::VERSION.to_string())
}