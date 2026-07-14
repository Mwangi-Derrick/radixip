package go

import (
	"net"
	"sync/atomic"
	"unsafe"
)

// IpNetwork represents a CIDR network
// Metadata represents the data stored with each route
type Metadata interface{}

// RadixEngine is a lock-free Radix IP engine
type RadixEngine struct {
	root unsafe.Pointer // *RadixNode
	size uint64
}

// NewRadixEngine creates a new empty engine
func NewRadixEngine() *RadixEngine {
	return &RadixEngine{
		root: unsafe.Pointer(newRadixNode()),
		size: 0,
	}
}


// Insert adds a subnet with metadata
func (e *RadixEngine) Insert(subnet string, metadata Metadata) error {
	// Parse subnet
	_, network, err := net.ParseCIDR(subnet)
	if err != nil {
		return ErrInvalidSubnet
	}

	// Copy-on-write: create new tree with insert
	oldRoot := (*radixNode)(atomic.LoadPointer(&e.root))
	newRoot := oldRoot.cloneWithInsert(network, metadata)

	// Atomic swap
	atomic.StorePointer(&e.root, unsafe.Pointer(newRoot))
	atomic.AddUint64(&e.size, 1)

	return nil
}