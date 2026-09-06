use radixip::{new_balanced, RadixEngine};
use radixip_config::RadixIpConfig;
use radixip_policy::{PackedIp, PolicyDecisionCode, PolicyHandle};
use std::ffi::CStr;
use std::os::raw::{c_char, c_int};
use std::ptr;
use std::sync::Arc;

#[repr(C)]
pub struct RadixPolicyHandle {
    inner: PolicyHandle,
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct RadixIpValue {
    pub family: u8,
    pub bytes: [u8; 16],
}

#[repr(C)]
#[derive(Clone, Copy)]
pub enum RadixPolicyDecision {
    Allow = 0,
    Block = 1,
    Limit = 2,
    BadRequest = 3,
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct RadixPolicyResult {
    pub decision: RadixPolicyDecision,
    pub retry_after_seconds: u32,
}

fn read_path(path: *const c_char) -> Result<String, c_int> {
    if path.is_null() {
        return Err(1);
    }
    let path = unsafe { CStr::from_ptr(path) };
    path.to_str().map(str::to_owned).map_err(|_| 2)
}

#[unsafe(no_mangle)]
pub extern "C" fn radix_policy_new_from_yaml(
    config_path: *const c_char,
    error_code: *mut c_int,
) -> *mut RadixPolicyHandle {
    let path = match read_path(config_path) {
        Ok(path) => path,
        Err(code) => {
            if !error_code.is_null() {
                unsafe { *error_code = code };
            }
            return ptr::null_mut();
        }
    };

    let config = match RadixIpConfig::from_file(&path) {
        Ok(config) => config,
        Err(_) => {
            if !error_code.is_null() {
                unsafe { *error_code = 3 };
            }
            return ptr::null_mut();
        }
    };

    let engine: Arc<Box<dyn RadixEngine>> = Arc::new(match tokio::runtime::Runtime::new() {
        Ok(runtime) => runtime.block_on(new_balanced()),
        Err(_) => {
            if !error_code.is_null() {
                unsafe { *error_code = 4 };
            }
            return ptr::null_mut();
        }
    });

    if !error_code.is_null() {
        unsafe { *error_code = 0 };
    }
    Box::into_raw(Box::new(RadixPolicyHandle {
        inner: PolicyHandle::new(engine, &config),
    }))
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn radix_policy_check(
    handle: *const RadixPolicyHandle,
    ip: *const RadixIpValue,
    result: *mut RadixPolicyResult,
) -> c_int {
    if handle.is_null() || ip.is_null() || result.is_null() {
        return 1;
    }

    let packed = unsafe { *(ip) };
    let Some(policy_result) = unsafe { &*handle }.inner.check(PackedIp {
        family: packed.family,
        bytes: packed.bytes,
    }) else {
        return 2;
    };

    let decision = match policy_result.decision {
        PolicyDecisionCode::Allow => RadixPolicyDecision::Allow,
        PolicyDecisionCode::Block => RadixPolicyDecision::Block,
        PolicyDecisionCode::Limit => RadixPolicyDecision::Limit,
        PolicyDecisionCode::BadRequest => RadixPolicyDecision::BadRequest,
    };
    unsafe {
        *result = RadixPolicyResult {
            decision,
            retry_after_seconds: policy_result.retry_after_seconds,
        };
    }
    0
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn radix_policy_free(handle: *mut RadixPolicyHandle) {
    if !handle.is_null() {
        unsafe {
            drop(Box::from_raw(handle));
        }
    }
}
