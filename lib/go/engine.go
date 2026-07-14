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
