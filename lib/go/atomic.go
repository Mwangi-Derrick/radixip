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