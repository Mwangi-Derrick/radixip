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

// LeafNode stores the actual value pointer plus the original prefix
// so that non-byte-aligned prefixes (e.g. /25, /17) can be matched
// correctly without false positives.
//
// Problem without this:
//
//	A /25 prefix is keyed on 3 full bytes (24 bits) plus 1 partial byte
//	(bit 25). ART naturally indexes by full bytes, so inserting 10.0.0.128/25
//	and 10.0.0.0/25 would both land at the same depth-3 slot keyed by the
//	first 3 bytes "10.0.0" — colliding on the partial 4th byte.
//
// Solution:
//
//	The tree traverses whole bytes up to floor(prefixLen/8).  At the
//	boundary byte (when prefixLen % 8 != 0) it uses only the significant
//	high bits as the index key, masking out the host bits.  At lookup time,
//	the leaf's PrefixLen and MaskedKey are checked so that an IP address
//	that shares the routed prefix bits matches, while one that differs in
//	the significant bits does not.
type LeafNode struct {
	Header Header

	// Value is a GC-pinned pointer to the caller-managed metadata.
	Value unsafe.Pointer

	// PrefixLen is the CIDR prefix length (0-128).
	// For IPv4 this is in the range 0-32.
	// And For IPV6 this is in the range 0-128.
	PrefixLen uint8

	// MaskedKey is the full IP address with all host bits zeroed,
	// stored as a [16]byte so both IPv4 (4 bytes significant) and
	// IPv6 (16 bytes significant) are handled uniformly.
	// In short it just stores the network prefix and zeros out the host bits
	MaskedKey [16]byte
}

// matches returns true if the IP address `ip` (as a slice of the same
// length as the original key) falls within this leaf's CIDR prefix.
func (l *LeafNode) matches(ip []byte) bool {
	fullBytes := int(l.PrefixLen) / 8
	remainBits := int(l.PrefixLen) % 8

	// Check every full byte.
	for i := 0; i < fullBytes && i < len(ip); i++ {
		if ip[i] != l.MaskedKey[i] {
			return false
		}
	}

	// Check the partial boundary byte (if any).
	if remainBits > 0 && fullBytes < len(ip) {
		mask := byte(0xFF << (8 - remainBits))
		if ip[fullBytes]&mask != l.MaskedKey[fullBytes]&mask {
			return false
		}
	}
	return true
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
	Index    [256]uint8 // Maps key byte → slot in Children; 0xFF = empty
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
	addChild(b byte, child unsafe.Pointer) Node // returns possibly-grown node
	removeChild(b byte) Node                    // returns possibly-shrunk node
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

// indexByteForDepth returns the routing byte used to navigate the trie
// at a given depth, masking out host bits for the boundary byte of a
// non-byte-aligned prefix so that different subnets within the same /N
// don't alias to the same child pointer.
//
//	depth     — current byte index (0-based)
//	prefixLen — CIDR prefix length
//	key       — full IP address bytes
func indexByteForDepth(key []byte, depth int, prefixLen uint8) byte {
	b := key[depth]
	fullBytes := int(prefixLen) / 8
	remainBits := int(prefixLen) % 8
	if depth == fullBytes && remainBits > 0 {
		// At the boundary byte: keep only the prefix bits.
		mask := byte(0xFF << (8 - remainBits))
		b &= mask
	}
	return b
}
