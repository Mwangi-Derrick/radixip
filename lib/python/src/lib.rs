use pyo3::prelude::*;
use pyo3::types::PyDict;
use radixip_core::RadixEngine;
use radixip_core::Metadata;
use std::collections::HashMap;

/// Python wrapper for RadixIP
#[pyclass]
struct PyRadixEngine {
    inner: RadixEngine,
}

#[pymethods]
impl PyRadixEngine {
    #[new]
    fn new() -> Self {
        Self {
            inner: RadixEngine::new(),
        }
    }

    fn insert(&mut self, subnet: &str, metadata: HashMap<String, String>) -> PyResult<()> {
        let meta = Metadata::from(metadata);
        self.inner.insert(subnet, meta)
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyValueError, _>(e.to_string()))?;
        Ok(())
    }

    fn match_ip(&self, ip: &str) -> PyResult<Option<HashMap<String, String>>> {
        match self.inner.match_ip(ip) {
            Some(meta) => {
                let map: HashMap<String, String> = meta.into();
                Ok(Some(map))
            }
            None => Ok(None),
        }
    }

    fn __len__(&self) -> PyResult<usize> {
        Ok(self.inner.size())
    }

    fn clear(&mut self) -> PyResult<()> {
        self.inner.clear();
        Ok(())
    }

    fn __repr__(&self) -> PyResult<String> {
        Ok(format!("RadixEngine(size={})", self.inner.size()))
    }
}

/// Python module
#[pymodule]
fn radixip(m: &Bound<'_, PyModule>) -> PyResult<()> {
    m.add_class::<PyRadixEngine>()?;
    m.add_function(wrap_pyfunction!(version, m)?)?;
    Ok(())
}

#[pyfunction]
fn version() -> PyResult<String> {
    Ok(radixip_core::VERSION.to_string())
}