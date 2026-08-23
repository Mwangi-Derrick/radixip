package radixip

import (
	"sync"
	"unsafe"
)

type AtomicNodeRef struct {
	ptr unsafe.Pointer // points to *Node
	mu  sync.RWMutex
}

// NewAtomicNodeRef creates a new AtomicNodeRef
func NewAtomicNodeRef() *AtomicNodeRef {
	return &AtomicNodeRef{
		ptr: nil,
	}
}

// Load returns the current node or nil if not set
func (a *AtomicNodeRef) Load() Node {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.ptr == nil {
		return nil
	}
	return *(*Node)(a.ptr)
}

// Store sets the current node
func (a *AtomicNodeRef) Store(node Node) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ptr = unsafe.Pointer(&node)
}

// CompareAndSwap performs a compare-and-swap operation
// It returns (success, oldValue)
// The oldValue is the current node if the swap failed
func (a *AtomicNodeRef) CompareAndSwap(current, new Node) (bool, Node) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.ptr == nil {
		return false, nil
	}

	existing := *(*Node)(a.ptr)

	// Compare by pointer equality
	if existing == current {
		a.ptr = unsafe.Pointer(&new)
		return true, new
	}

	return false, existing
}

// Default implementation
func DefaultAtomicNodeRef() *AtomicNodeRef {
	return NewAtomicNodeRef()
}
