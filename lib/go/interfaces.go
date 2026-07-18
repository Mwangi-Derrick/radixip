package go

import "net"

// EngineVariant represents the type of engine to use
type EngineVariant string

const (
	EngineStandard    EngineVariant = "standard"
	EngineConcurrent  EngineVariant = "concurrent"
	EngineLockFree    EngineVariant = "lockfree"
	EngineAdaptive    EngineVariant = "adaptive"
)

// NodeVariant represents the type of node implementation
type NodeVariant string

const (
	NodeNormal   NodeVariant = "normal"
	NodeAtomic   NodeVariant = "atomic"
	NodeLockFree NodeVariant = "lockfree"
	NodePadded   NodeVariant = "padded"
)


//  RADIX NODE INTERFACE 
type RadixNode interface {
    Bit() *uint8
    Left() RadixNode
    Right() RadixNode
    Metadata() *Metadata
    Prefix() *net.IPNet
    SetLeft(node RadixNode)
    SetRight(node RadixNode)
    SetMetadata(metadata *Metadata)
    ClearMetadata()
    SetBit(bit uint8)
    SetPrefix(prefix *net.IPNet)
}

//  RADIX ENGINE INTERFACE 
type RadixEngine interface {
    Insert(prefix *net.IPNet, metadata Metadata) error
    Lookup(ip net.IP) *Metadata
    Remove(prefix *net.IPNet) *Metadata
    Contains(prefix *net.IPNet) bool
    Clear()
    Size() int
    Stats() *EngineStats
}


//  ROUTE TREE INTERFACE 
// Separates the routing logic from the concurrency engine
type RouteTree interface {
    Insert(prefix IpNetwork, metadata Metadata) (bool, error) // Returns true if it was a new prefix
    Lookup(ip *net.IP) *Metadata
    Remove(prefix *IpNetwork) *Metadata
    Contains(prefix *IpNetwork) bool
    Clear()
}

// FACTORY INTERFACE 
type EngineFactory interface {
    CreateEngine(variant EngineVariant) RadixEngine
    CreateNode(variant NodeVariant) RadixNode
}