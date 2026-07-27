use pyo3::exceptions::{PyTypeError, PyValueError};
use pyo3::prelude::*;
use pyo3::types::PyDict;
use radixip::{new_balanced, Metadata, RadixConfig, RadixEngine};
use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::Arc;
use tokio;

// ---------------------------------------------------------------------------
// Internal helper: Metadata <-> Python dict
// ---------------------------------------------------------------------------

fn meta_from_dict(dict: &Bound<'_, PyDict>) -> PyResult<Metadata> {
    let value: String = dict
        .get_item("value")?
        .map(|v| v.extract::<String>())
        .transpose()?
        .unwrap_or_default();

    let mut attributes: HashMap<String, String> = HashMap::new();
    if let Some(attrs) = dict.get_item("attributes")? {
        let attrs_dict: &Bound<'_, PyDict> = attrs
            .downcast()
            .map_err(|_| PyTypeError::new_err("'attributes' must be a dict"))?;
        for (k, v) in attrs_dict.iter() {
            attributes.insert(k.extract()?, v.extract()?);
        }
    }

    Ok(Metadata { value, attributes })
}

fn meta_to_dict(py: Python<'_>, meta: Metadata) -> PyResult<PyObject> {
    let dict = PyDict::new(py);
    dict.set_item("value", meta.value)?;
    let attrs = PyDict::new(py);
    for (k, v) in meta.attributes {
        attrs.set_item(k, v)?;
    }
    dict.set_item("attributes", attrs)?;
    Ok(dict.into())
}

// ---------------------------------------------------------------------------
// PyRadixEngine
// ---------------------------------------------------------------------------

/// High-performance IP radix-tree engine.
///
/// Example::
///
///     from radixip import RadixEngine
///
///     engine = RadixEngine()
///     engine.insert("10.0.0.0/8", {"value": "allow", "attributes": {"asn": "AS1234"}})
///
///     match = engine.lookup("10.1.2.3")
///     print(match["value"])   # "allow"
///
#[pyclass(name = "RadixEngine")]
pub struct PyRadixEngine {
    inner: Arc<Box<dyn RadixEngine>>,
}

#[pymethods]
impl PyRadixEngine {
    /// Create a new balanced RadixEngine.
    ///
    /// Parameters
    /// ----------
    /// variant : str, optional
    ///     One of "standard", "concurrent", "lockfree", "adaptive".
    #[new]
    #[pyo3(signature = (variant=None))]
    fn new(variant: Option<String>) -> PyResult<Self> {
        let config = match variant.as_deref() {
            Some("standard") => {
                let mut c = RadixConfig::new();
                c.engine_variant = radixip::EngineVariant::Standard;
                c
            }
            Some("lockfree") => {
                let mut c = RadixConfig::new();
                c.engine_variant = radixip::EngineVariant::LockFree;
                c
            }
            Some("adaptive") => {
                let mut c = RadixConfig::new();
                c.engine_variant = radixip::EngineVariant::Adaptive;
                c
            }
            _ => RadixConfig::memory_efficient(),
        };

        // Block on the async constructor
        let inner = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .map_err(|e| PyValueError::new_err(e.to_string()))?
            .block_on(radixip::new(config));

        Ok(Self {
            inner: Arc::new(inner),
        })
    }

    /// Insert a CIDR prefix with associated metadata dict.
    ///
    /// Parameters
    /// ----------
    /// subnet : str
    ///     CIDR notation, e.g. ``"10.0.0.0/8"``.
    /// metadata : dict
    ///     Must contain at least ``{"value": str, "attributes": dict}``.
    fn insert(&self, py: Python<'_>, subnet: String, metadata: &Bound<'_, PyDict>) -> PyResult<()> {
        let _ = py;
        let prefix = subnet
            .parse::<ipnetwork::IpNetwork>()
            .map_err(|e| PyValueError::new_err(format!("Invalid CIDR '{subnet}': {e}")))?;
        let meta = meta_from_dict(metadata)?;
        self.inner
            .insert(prefix, meta)
            .map_err(|e| PyValueError::new_err(e))
    }

    /// Longest-prefix match.
    ///
    /// Returns a metadata dict on match, or ``None``.
    fn lookup(&self, py: Python<'_>, ip: String) -> PyResult<Option<PyObject>> {
        let addr = ip
            .parse::<IpAddr>()
            .map_err(|_| PyValueError::new_err(format!("Invalid IP address: {ip}")))?;
        match self.inner.lookup(&addr) {
            Some(meta) => Ok(Some(meta_to_dict(py, meta)?)),
            None => Ok(None),
        }
    }

    /// Remove a prefix.  Returns ``True`` if the entry existed.
    fn remove(&self, subnet: String) -> PyResult<bool> {
        let prefix = subnet
            .parse::<ipnetwork::IpNetwork>()
            .map_err(|e| PyValueError::new_err(format!("Invalid CIDR '{subnet}': {e}")))?;
        Ok(self.inner.remove(&prefix).is_some())
    }

    /// Returns ``True`` if any stored prefix covers the given IP.
    fn contains(&self, ip: String) -> PyResult<bool> {
        let addr = ip
            .parse::<IpAddr>()
            .map_err(|_| PyValueError::new_err(format!("Invalid IP address: {ip}")))?;
        Ok(self.inner.lookup(&addr).is_some())
    }

    /// Remove all entries.
    fn clear(&self) {
        self.inner.clear();
    }

    /// Number of stored prefixes.
    fn __len__(&self) -> usize {
        self.inner.size()
    }

    /// Engine performance statistics.
    fn stats(&self, py: Python<'_>) -> PyResult<PyObject> {
        let s = self.inner.stats();
        let dict = PyDict::new(py);
        dict.set_item("size", s.size)?;
        dict.set_item("inserts", s.inserts)?;
        dict.set_item("lookups", s.lookups)?;
        dict.set_item("hits", s.hits)?;
        dict.set_item("misses", s.misses)?;
        dict.set_item("removals", s.removals)?;
        Ok(dict.into())
    }

    fn __repr__(&self, py: Python<'_>) -> PyResult<PyObject> {
        let s = format!("RadixEngine(size={})", self.inner.size());
        Ok(s.to_object(py))
    }
}

// ---------------------------------------------------------------------------
// Module
// ---------------------------------------------------------------------------

/// RadixIP — high-performance IP subnet longest-prefix matching engine.
#[pymodule]
#[pyo3(name = "radixip")]
fn radixip_module(m: &Bound<'_, PyModule>) -> PyResult<()> {
    m.add_class::<PyRadixEngine>()?;
    m.add_function(wrap_pyfunction!(py_version, m)?)?;
    Ok(())
}

#[pyfunction]
#[pyo3(name = "version")]
fn py_version() -> String {
    radixip::VERSION.to_string()
}
