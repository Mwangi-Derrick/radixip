package radixip

import "net"

// EngineVariant represents the type of engine to use
type EngineVariant string

const (
	EngineStandard   EngineVariant = "standard"
	EngineConcurrent EngineVariant = "concurrent"
	EngineLockFree   EngineVariant = "lockfree"
	EngineAdaptive   EngineVariant = "adaptive"
	EngineART        EngineVariant = "art"
	EngineConcurrentART EngineVariant = "concurrentart"
)

// NodeVariant represents the type of node implementation
type NodeVariant string

const (
	NormalTrieNode             NodeVariant = "normaltrie"
	AtomicTrieNode             NodeVariant = "atomictrie"
	LockFreeTrieNode           NodeVariant = "lockfreetrie"
	PaddedTrieNode             NodeVariant = "paddedtrie"
	NormalRadixNode   NodeVariant = "normalradix"
	PaddedRadixNode   NodeVariant = "paddedradix"
	AtomicRadixNode   NodeVariant = "atomicradix"
	LockFreeRadixNode NodeVariant = "lockfreeradix"
)

// RADIX NODE INTERFACE
type Node interface {
	Bit() *uint8
	Left() Node
	Right() Node
	Metadata() *Metadata
	Prefix() *net.IPNet
	SetLeft(node Node)
	SetRight(node Node)
	SetMetadata(metadata *Metadata)
	ClearMetadata()
	SetBit(bit uint8)
	SetPrefix(prefix *net.IPNet)

	// Compressed-node methods (uncompressed nodes return default/nil/0)
	EdgeBits() []byte
	EdgeLen() int
	SetEdge(bits []byte, length int)
}

// RADIX ENGINE INTERFACE
type RadixEngine interface {
	Insert(prefix *net.IPNet, metadata Metadata) error
	Lookup(ip net.IP) *Metadata
	Remove(prefix *net.IPNet) *Metadata
	Contains(prefix *net.IPNet) bool
	Clear()
	Size() int
	Stats() *EngineStats
}

//	ROUTE TREE INTERFACE
//
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
	CreateNode(variant NodeVariant) Node
}

// NodeBuilder is a factory for Nodes
type NodeBuilder struct {
	variant NodeVariant
}

func NewNodeBuilder(variant NodeVariant) *NodeBuilder {
	return &NodeBuilder{variant: variant}
}

func (b *NodeBuilder) Build() Node {
	switch b.variant {
	case NormalTrieNode:
		return NewNormalNode()
	case AtomicTrieNode:
		return NewAtomicNode()
	case PaddedTrieNode:
		return NewPaddedNode()
	case LockFreeTrieNode:
		return NewLockFreeNode()
	case NormalRadixNode:
		return NewCompressedNormalNode()
	case AtomicRadixNode:
		return NewCompressedAtomicNode()
	case PaddedRadixNode:
		return NewCompressedPaddedNode()
	case LockFreeRadixNode:
		return NewCompressedLockFreeNode()
	default:
		return NewAtomicNode()
	}
}
