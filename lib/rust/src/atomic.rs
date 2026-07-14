use std::sync::{Arc, RwLock};

use crate::traits::RadixNode;

// Atomic reference counting utilities for lock-free operations
pub struct AtomicNodeRef {
    ptr: RwLock<Option<Arc<dyn RadixNode>>>,
}

impl AtomicNodeRef {
    pub fn new() -> Self {
        Self {
            ptr: RwLock::new(None),
        }
    }

    pub fn load(&self) -> Option<Arc<dyn RadixNode>> {
        self.ptr.read().unwrap().clone()
    }

    pub fn store(&self, node: Arc<dyn RadixNode>) {
        *self.ptr.write().unwrap() = Some(node);
    }

    pub fn compare_exchange(
        &self,
        current: Arc<dyn RadixNode>,
        new: Arc<dyn RadixNode>,
    ) -> std::result::Result<Arc<dyn RadixNode>, Arc<dyn RadixNode>> {
        let mut guard = self.ptr.write().unwrap();
        match guard.as_ref() {
            Some(existing) if Arc::ptr_eq(existing, &current) => {
                *guard = Some(new.clone());
                Ok(new)
            }
            Some(existing) => Err(existing.clone()),
            None => Err(new),
        }
    }
}

impl Default for AtomicNodeRef {
    fn default() -> Self {
        Self::new()
    }
}
