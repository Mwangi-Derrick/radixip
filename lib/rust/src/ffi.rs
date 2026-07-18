// radixip-rs/src/ffi.rs
// C-FFI Bindings for RadixIP

use std::ffi::{CStr, CString};
use std::net::IpAddr;
use std::os::raw::{c_char, c_int};
use std::ptr;

use ipnetwork::IpNetwork;

use crate::{Metadata, RadixEngine, new_balanced};

/// Opaque handle to a RadixEngine
#[repr(C)]
pub struct RadixEngineHandle {
    inner: Box<dyn RadixEngine>,
}

fn read_c_str(ptr: *const c_char) -> Option<String> {
    if ptr.is_null() {
        return None;
    }

    unsafe { CStr::from_ptr(ptr).to_str().ok().map(str::to_string) }
}

/// Create a new RadixEngine
#[unsafe(no_mangle)]
pub async extern "C" fn radix_engine_new() -> *mut RadixEngineHandle {
    Box::into_raw(Box::new(RadixEngineHandle {
        inner: new_balanced().await,
    }))
}

/// Free a RadixEngine
#[unsafe(no_mangle)]
pub unsafe extern "C" fn radix_engine_free(handle: *mut RadixEngineHandle) {
    if !handle.is_null() {
        unsafe {
            drop(Box::from_raw(handle));
        }
    }
}

/// Insert a subnet into the engine
#[unsafe(no_mangle)]
pub unsafe extern "C" fn radix_engine_insert(
    handle: *mut RadixEngineHandle,
    subnet: *const c_char,
    metadata: *const c_char,
) -> c_int {
    if handle.is_null() {
        return -1;
    }

    let Some(subnet_str) = read_c_str(subnet) else {
        return -1;
    };
    let Some(metadata_str) = read_c_str(metadata) else {
        return -1;
    };

    let prefix = match subnet_str.parse::<IpNetwork>() {
        Ok(prefix) => prefix,
        Err(_) => return -1,
    };

    // Parse metadata as JSON first, then fall back to a plain string value.
    let metadata = serde_json::from_str::<Metadata>(&metadata_str)
        .unwrap_or_else(|_| Metadata::new(metadata_str));

    let handle = unsafe { &*handle };
    match handle.inner.insert(prefix, metadata) {
        Ok(()) => 0,
        Err(_) => -1,
    }
}

/// Match an IP address against the engine
#[unsafe(no_mangle)]
pub unsafe extern "C" fn radix_engine_match(
    handle: *const RadixEngineHandle,
    ip: *const c_char,
) -> *mut c_char {
    if handle.is_null() {
        return ptr::null_mut();
    }

    let Some(ip_str) = read_c_str(ip) else {
        return ptr::null_mut();
    };

    let ip = match ip_str.parse::<IpAddr>() {
        Ok(ip) => ip,
        Err(_) => return ptr::null_mut(),
    };

    let handle = unsafe { &*handle };
    match handle.inner.lookup(&ip) {
        Some(metadata) => serde_json::to_string(&metadata)
            .ok()
            .and_then(|json| CString::new(json).ok())
            .map(CString::into_raw)
            .unwrap_or(ptr::null_mut()),
        None => ptr::null_mut(),
    }
}

/// Free a C-string returned by radix_engine_match
#[unsafe(no_mangle)]
pub unsafe extern "C" fn radix_engine_free_string(ptr: *mut c_char) {
    if !ptr.is_null() {
        unsafe {
            drop(CString::from_raw(ptr));
        }
    }
}

/// Check if an IP matches (boolean version)
#[unsafe(no_mangle)]
pub unsafe extern "C" fn radix_engine_contains(
    handle: *const RadixEngineHandle,
    ip: *const c_char,
) -> bool {
    if handle.is_null() {
        return false;
    }

    let Some(ip_str) = read_c_str(ip) else {
        return false;
    };
    let Ok(ip) = ip_str.parse::<IpAddr>() else {
        return false;
    };

    let handle = unsafe { &*handle };
    handle.inner.lookup(&ip).is_some()
}

/// Get the size of the engine (number of subnets)
#[unsafe(no_mangle)]
pub unsafe extern "C" fn radix_engine_size(handle: *const RadixEngineHandle) -> usize {
    if handle.is_null() {
        return 0;
    }

    let handle = unsafe { &*handle };
    handle.inner.size()
}

/// Clear all subnets from the engine
#[unsafe(no_mangle)]
pub unsafe extern "C" fn radix_engine_clear(handle: *mut RadixEngineHandle) {
    if !handle.is_null() {
        let handle = unsafe { &*handle };
        handle.inner.clear();
    }
}

/// Get the library version
#[unsafe(no_mangle)]
pub extern "C" fn radix_engine_version() -> *const c_char {
    concat!(env!("CARGO_PKG_VERSION"), "\0").as_ptr() as *const c_char
}
