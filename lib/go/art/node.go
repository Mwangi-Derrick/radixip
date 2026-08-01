package art

import "unsafe"

// NodeType tag embedded at offset 0 of every node struct so nodeToNode()
// can type-switch without an extra allocation.
type NodeType uint8

const (
	TypeNode4   NodeType = iota // 0
	TypeNode16                  // 1
	TypeNode48                  // 2
	TypeNode256                 // 3
	TypeLeaf                    // 4
)

// Header is the first field of every node struct (same offset, same size).
// nodeToNode() reads Header.Type to decide the concrete type.
type Header struct {
	Type        NodeType
	NumChildren uint8
	PrefixLen   uint8
	Prefix      [8]byte // inline path-compression prefix (up to 8 bytes)
}

// LeafNode stores the actual value pointer.
type LeafNode struct {
	Value unsafe.Pointer
}

// ---- Node4 ---- (1-4 children, linear key scan)

type Node4 struct {
	Header   Header
	Keys     [4]byte
	Children [4]unsafe.Pointer
}

// ---- Node16 ---- (5-16 children, linear or SIMD key scan)

type Node16 struct {
	Header   Header
	Keys     [16]byte
	Children [16]unsafe.Pointer
}

// ---- Node48 ---- (17-48 children, 256-byte index array)

type Node48 struct {
	Header   Header
	Index    [256]uint8     // Maps key byte → slot in Children; 0xFF = empty
	Children [48]unsafe.Pointer
}

// ---- Node256 ---- (49-256 children, direct array)

type Node256 struct {
	Header   Header
	Children [256]unsafe.Pointer
}

// Node is the unified interface every node type must satisfy.
// All mutating methods return the (possibly replaced) node so callers
// can handle grow/shrink transparently.
type Node interface {
	findChild(b byte) (unsafe.Pointer, bool)
	addChild(b byte, child unsafe.Pointer) Node   // returns possibly-grown node
	removeChild(b byte) Node                      // returns possibly-shrunk node
	isFull() bool
	isEmpty() bool
	numChildren() int
}

// nodeToNode reads the NodeType tag at the start of p and returns the
// concrete Node implementation without any allocation.
func nodeToNode(p unsafe.Pointer) Node {
	if p == nil {
		return nil
	}
	switch (*Header)(p).Type {
	case TypeNode4:
		return (*Node4)(p)
	case TypeNode16:
		return (*Node16)(p)
	case TypeNode48:
		return (*Node48)(p)
	case TypeNode256:
		return (*Node256)(p)
	default:
		return nil
	}
}

// asPtr converts a Node back to unsafe.Pointer without boxing.
// Works because all node types are pointer receivers.
func asPtr(n Node) unsafe.Pointer {
	switch v := n.(type) {
	case *Node4:
		return unsafe.Pointer(v)
	case *Node16:
		return unsafe.Pointer(v)
	case *Node48:
		return unsafe.Pointer(v)
	case *Node256:
		return unsafe.Pointer(v)
	default:
		return nil
	}
}