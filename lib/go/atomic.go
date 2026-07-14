package go

import ("sync"
    "unsafe")


type AtomicNodeRef struct {
	ptr unsafe.Pointer // points to *RadixNode
	mu  sync.RWMutex
}

// NewAtomicNodeRef creates a new AtomicNodeRef
func NewAtomicNodeRef() *AtomicNodeRef {
	return &AtomicNodeRef{
		ptr: nil,
	}
}

// Load returns the current node or nil if not set
func (a *AtomicNodeRef) Load() RadixNode {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	if a.ptr == nil {
		return nil
	}
	return *(*RadixNode)(a.ptr)
}

// Store sets the current node
func (a *AtomicNodeRef) Store(node RadixNode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ptr = unsafe.Pointer(&node)
}