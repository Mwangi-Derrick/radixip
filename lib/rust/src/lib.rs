//! RadixIP - High-performance IP subnet caching engine
//!
//! This library provides a lock-free binary radix tree for
//! longest-prefix matching of IP addresses against CIDR blocks.

pub mod engine;
pub mod node;
pub mod lpm;
pub mod cache;
pub mod types;
pub mod errors;
pub mod traits;
pub mod atomic;
pub mod config;


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