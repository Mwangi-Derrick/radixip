use std::sync::{Arc, RwLock};

use crate::traits::Node;

// Atomic reference counting utilities for lock-free operations
pub struct AtomicNodeRef {
    ptr: RwLock<Option<Arc<dyn Node>>>,
}

impl AtomicNodeRef {
    pub fn new() -> Self {
        Self {
            ptr: RwLock::new(None),
        }
    }

    pub fn load(&self) -> Option<Arc<dyn Node>> {
        self.ptr.read().unwrap().clone()
    }

    pub fn store(&self, node: Arc<dyn Node>) {
        *self.ptr.write().unwrap() = Some(node);
    }

    pub fn compare_exchange(
        &self,
        current: Arc<dyn Node>,
        new: Arc<dyn Node>,
    ) -> std::result::Result<Arc<dyn Node>, Arc<dyn Node>> {
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
