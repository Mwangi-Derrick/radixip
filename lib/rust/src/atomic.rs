use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

// Atomic reference counting utilities for lock-free operations
pub struct AtomicNodeRef {
    ptr: AtomicU64,
}

impl AtomicNodeRef {
    pub fn new() -> Self {
        Self {
            ptr: AtomicU64::new(0),
        }
    }
    
    pub fn load(&self) -> Option<Arc<dyn RadixNode>> {
        let bits = self.ptr.load(Ordering::Acquire);
        if bits == 0 {
            None
        } else {
            // Decode pointer from bits
            unsafe {
                let ptr = bits as *const dyn RadixNode;
                Some(Arc::from_raw(ptr))
            }
        }
    }
    
    pub fn store(&self, node: Arc<dyn RadixNode>) {
        let ptr = Arc::into_raw(node) as u64;
        let old = self.ptr.swap(ptr, Ordering::Release);
        if old != 0 {
            unsafe {
                drop(Arc::from_raw(old as *const dyn RadixNode));
            }
        }
    }
    
    pub fn compare_exchange(&self, current: Arc<dyn RadixNode>, new: Arc<dyn RadixNode>) -> Result<Arc<dyn RadixNode>, Arc<dyn RadixNode>> {
        let current_ptr = Arc::into_raw(current) as u64;
        let new_ptr = Arc::into_raw(new) as u64;
        
        match self.ptr.compare_exchange(current_ptr, new_ptr, Ordering::Release, Ordering::Acquire) {
            Ok(_) => unsafe {
                // Free the current pointer since it's no longer used
                drop(Arc::from_raw(current_ptr as *const dyn RadixNode));
                Ok(Arc::from_raw(new_ptr as *const dyn RadixNode))
            },
            Err(actual) => {
                // Free the new pointer since it wasn't used
                unsafe {
                    drop(Arc::from_raw(new_ptr as *const dyn RadixNode));
                }
                unsafe {
                    Err(Arc::from_raw(actual as *const dyn RadixNode))
                }
            }
        }
    }
}

unsafe impl Send for AtomicNodeRef {}
unsafe impl Sync for AtomicNodeRef {}