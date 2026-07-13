// radixip-rs/src/ffi.rs
// C-FFI Bindings for RadixIP

use std::ffi::{CStr, CString};
use std::os::raw::{c_char, c_int, c_void};
use std::ptr;

use crate::engine::RadixEngine;
use crate::types::Metadata;

/// Opaque handle to a RadixEngine
#[repr(C)]
pub struct RadixEngineHandle {
    inner: RadixEngine,
}

/// Create a new RadixEngine
#[no_mangle]
pub extern "C" fn radix_engine_new() -> *mut RadixEngineHandle {
    let engine = RadixEngine::new();
    Box::into_raw(Box::new(RadixEngineHandle { inner: engine }))
}

/// Free a RadixEngine
#[no_mangle]
pub unsafe extern "C" fn radix_engine_free(handle: *mut RadixEngineHandle) {
    if !handle.is_null() {
        drop(Box::from_raw(handle));
    }
}

/// Insert a subnet into the engine
#[no_mangle]
pub unsafe extern "C" fn radix_engine_insert(
    handle: *mut RadixEngineHandle,
    subnet: *const c_char,
    metadata: *const c_char,
) -> c_int {
    if handle.is_null() || subnet.is_null() || metadata.is_null() {
        return -1;
    }
    
    let subnet_str = match CStr::from_ptr(subnet).to_str() {
        Ok(s) => s,
        Err(_) => return -1,
    };
    
    let metadata_str = match CStr::from_ptr(metadata).to_str() {
        Ok(s) => s,
        Err(_) => return -1,
    };
    
    // Parse metadata (JSON or custom format)
    match serde_json::from_str(metadata_str) {
        Ok(meta) => {
            let handle = &mut *handle;
            handle.inner.insert(subnet_str, meta);
            0 // Success
        }
        Err(_) => -1,
    }
}

/// Match an IP address against the engine
#[no_mangle]
pub unsafe extern "C" fn radix_engine_match(
    handle: *const RadixEngineHandle,
    ip: *const c_char,
) -> *const c_char {
    if handle.is_null() || ip.is_null() {
        return ptr::null();
    }
    
    let ip_str = match CStr::from_ptr(ip).to_str() {
        Ok(s) => s,
        Err(_) => return ptr::null(),
    };
    
    let handle = &*handle;
    if let Some(metadata) = handle.inner.match_ip(ip_str) {
        // Serialize metadata to JSON string
        match serde_json::to_string(&metadata) {
            Ok(json) => {
                // Return a C-string (caller must free)
                CString::new(json)
                    .ok()
                    .map(|cstr| cstr.into_raw())
                    .unwrap_or(ptr::null())
            }
            Err(_) => ptr::null(),
        }
    } else {
        ptr::null()
    }
}

/// Free a C-string returned by radix_engine_match
#[no_mangle]
pub unsafe extern "C" fn radix_engine_free_string(ptr: *mut c_char) {
    if !ptr.is_null() {
        drop(CString::from_raw(ptr));
    }
}

/// Check if an IP matches (boolean version)
#[no_mangle]
pub unsafe extern "C" fn radix_engine_contains(
    handle: *const RadixEngineHandle,
    ip: *const c_char,
) -> bool {
    if handle.is_null() || ip.is_null() {
        return false;
    }
    
    let ip_str = match CStr::from_ptr(ip).to_str() {
        Ok(s) => s,
        Err(_) => return false,
    };
    
    let handle = &*handle;
    handle.inner.match_ip(ip_str).is_some()
}

/// Get the size of the engine (number of subnets)
#[no_mangle]
pub unsafe extern "C" fn radix_engine_size(handle: *const RadixEngineHandle) -> usize {
    if handle.is_null() {
        return 0;
    }
    let handle = &*handle;
    handle.inner.size()
}

/// Clear all subnets from the engine
#[no_mangle]
pub unsafe extern "C" fn radix_engine_clear(handle: *mut RadixEngineHandle) {
    if !handle.is_null() {
        let handle = &mut *handle;
        handle.inner.clear();
    }
}