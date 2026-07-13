//! RadixIP - High-performance IP subnet caching engine
//!
//! This library provides a lock-free binary radix tree for
//! longest-prefix matching of IP addresses against CIDR blocks.

mod engine;
mod node;
mod lpm;
mod cache;
mod types;
mod errors;

#[cfg(feature = "redis")]
mod redis;

#[cfg(feature = "ffi")]
pub mod ffi;

#[cfg(feature = "pyo3")]
pub mod python;

#[cfg(feature = "node")]
pub mod nodejs;

pub use engine::RadixEngine;
pub use types::{Metadata, SubnetRule};
pub use errors::{RadixError, Result};

/// Library version
pub const VERSION: &str = env!("CARGO_PKG_VERSION");

/// Pre-configured engine with default settings
pub fn new() -> RadixEngine {
    RadixEngine::new()
}