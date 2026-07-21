package node

import (
	"net"
	"sync"

	radix "github.com/Mwangi-Derrick/radixip/lib/go"
)

//
// COMPRESSED TREE  (Patricia / Radix trie)
//
// Each node stores a compressed bit-string edge.
// Non-branching chains are folded into single nodes, so O(k)
// where k = branching points, not prefix length.

type compressedNode struct {
	mu       sync.Mutex
	edgeBits []byte
	edgeLen  int
	metadata *radix.Metadata
	prefix   *net.IPNet
	left     *compressedNode
	right    *compressedNode
}

// CompressedTree is a Patricia / compressed radix trie.
// It is safe for concurrent use via per-node mutexes (fine-grained locking).
type CompressedTree struct {
	root *compressedNode
}

func NewCompressedTree(_ radix.NodeVariant) *CompressedTree {
	return &CompressedTree{root: &compressedNode{}}
}

func (t *CompressedTree) insertNode(n *compressedNode, key []byte, keyLen, depth int, prefix *net.IPNet, meta *radix.Metadata) bool {
	n.mu.Lock()

	remaining := keyLen - depth
	if remaining < 0 {
		remaining = 0
	}

	// Empty node — store directly
	if n.edgeLen == 0 && n.metadata == nil && n.left == nil && n.right == nil {
		n.edgeBits = extractBits(key, depth, remaining)
		n.edgeLen = remaining
		n.prefix = prefix
		isNew := n.metadata == nil
		n.metadata = meta
		n.mu.Unlock()
		return isNew
	}

	keyRem := extractBits(key, depth, remaining)
	shared := commonPrefixLen(n.edgeBits, n.edgeLen, keyRem, remaining)

	// Exact match
	if shared == n.edgeLen && shared == remaining {
		isNew := n.metadata == nil
		n.metadata = meta
		n.prefix = prefix
		n.mu.Unlock()
		return isNew
	}

	// Partial match — split
	if shared < n.edgeLen {
		pivotBit := getBitFromBytes(n.edgeBits, shared)
		// New child carries current edge's remainder
		child := &compressedNode{
			edgeBits: extractBits(n.edgeBits, shared+1, n.edgeLen-shared-1),
			edgeLen:  n.edgeLen - shared - 1,
			metadata: n.metadata,
			prefix:   n.prefix,
			left:     n.left,
			right:    n.right,
		}
		// Trim current node to shared prefix
		n.edgeBits = extractBits(n.edgeBits, 0, shared)
		n.edgeLen = shared
		n.metadata = nil
		n.prefix = nil
		n.left = nil
		n.right = nil

		if pivotBit == 0 {
			n.left = child
		} else {
			n.right = child
		}

		if shared == remaining {
			n.metadata = meta
			n.prefix = prefix
			n.mu.Unlock()
			return true
		}

		newBit := getBitFromBytes(keyRem, shared)
		newLeafEdge := extractBits(keyRem, shared+1, remaining-shared-1)
		newLeaf := &compressedNode{
			edgeBits: newLeafEdge,
			edgeLen:  remaining - shared - 1,
			metadata: meta,
			prefix:   prefix,
		}
		if newBit == 0 {
			n.left = newLeaf
		} else {
			n.right = newLeaf
		}
		n.mu.Unlock()
		return true
	}

	// Descend
	nextBit := getBitFromBytes(keyRem, shared)
	var childPtr **compressedNode
	if nextBit == 0 {
		childPtr = &n.left
	} else {
		childPtr = &n.right
	}

	if *childPtr == nil {
		newDepth := depth + shared + 1
		newRemaining := keyLen - newDepth
		if newRemaining < 0 {
			newRemaining = 0
		}
		newLeaf := &compressedNode{
			edgeBits: extractBits(key, newDepth, newRemaining),
			edgeLen:  newRemaining,
			metadata: meta,
			prefix:   prefix,
		}
		*childPtr = newLeaf
		n.mu.Unlock()
		return true
	}

	child := *childPtr
	n.mu.Unlock()
	return t.insertNode(child, key, keyLen, depth+shared+1, prefix, meta)
}

func (t *CompressedTree) lookupNode(n *compressedNode, key []byte, depth int) *radix.Metadata {
	if n == nil {
		return nil
	}
	n.mu.Lock()

	remaining := len(key)*8 - depth
	if remaining < 0 {
		remaining = 0
	}
	keyRem := extractBits(key, depth, remaining)
	shared := commonPrefixLen(n.edgeBits, n.edgeLen, keyRem, remaining)

	if shared < n.edgeLen {
		n.mu.Unlock()
		return nil
	}

	best := n.metadata
	newDepth := depth + shared
	var nextChild *compressedNode
	if newDepth < len(key)*8 {
		nextBit := getBitFromBytes(key, newDepth)
		if nextBit == 0 {
			nextChild = n.left
		} else {
			nextChild = n.right
		}
	}
	n.mu.Unlock()

	if nextChild != nil {
		if deeper := t.lookupNode(nextChild, key, newDepth+1); deeper != nil {
			best = deeper
		}
	}
	return best
}

func (t *CompressedTree) removeNode(n *compressedNode, key []byte, keyLen, depth int) *radix.Metadata {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	remaining := keyLen - depth
	if remaining < 0 {
		remaining = 0
	}
	keyRem := extractBits(key, depth, remaining)
	shared := commonPrefixLen(n.edgeBits, n.edgeLen, keyRem, remaining)

	if shared < n.edgeLen {
		n.mu.Unlock()
		return nil
	}

	if shared == remaining {
		removed := n.metadata
		n.metadata = nil
		n.prefix = nil
		n.mu.Unlock()
		return removed
	}

	nextBit := getBitFromBytes(keyRem, shared)
	var child *compressedNode
	if nextBit == 0 {
		child = n.left
	} else {
		child = n.right
	}
	n.mu.Unlock()
	return t.removeNode(child, key, keyLen, depth+shared+1)
}

func (t *CompressedTree) containsNode(n *compressedNode, key []byte, keyLen, depth int) bool {
	if n == nil {
		return false
	}
	n.mu.Lock()
	remaining := keyLen - depth
	if remaining < 0 {
		remaining = 0
	}
	keyRem := extractBits(key, depth, remaining)
	shared := commonPrefixLen(n.edgeBits, n.edgeLen, keyRem, remaining)

	if shared < n.edgeLen {
		n.mu.Unlock()
		return false
	}
	if shared == remaining {
		found := n.metadata != nil
		n.mu.Unlock()
		return found
	}
	nextBit := getBitFromBytes(keyRem, shared)
	var child *compressedNode
	if nextBit == 0 {
		child = n.left
	} else {
		child = n.right
	}
	n.mu.Unlock()
	return t.containsNode(child, key, keyLen, depth+shared+1)
}

func (t *CompressedTree) Insert(prefix radix.IpNetwork, metadata radix.Metadata) (bool, error) {
	key := ipToBytes(prefix.IP)
	ones, _ := prefix.Mask.Size()
	netPrefix := net.IPNet{IP: prefix.IP, Mask: prefix.Mask}
	return t.insertNode(t.root, key, ones, 0, &netPrefix, &metadata), nil
}

func (t *CompressedTree) Lookup(ip *net.IP) *radix.Metadata {
	key := ipToBytes(*ip)
	return t.lookupNode(t.root, key, 0)
}

func (t *CompressedTree) Remove(prefix *radix.IpNetwork) *radix.Metadata {
	key := ipToBytes(prefix.IP)
	ones, _ := prefix.Mask.Size()
	return t.removeNode(t.root, key, ones, 0)
}

func (t *CompressedTree) Contains(prefix *radix.IpNetwork) bool {
	key := ipToBytes(prefix.IP)
	ones, _ := prefix.Mask.Size()
	return t.containsNode(t.root, key, ones, 0)
}

func (t *CompressedTree) Clear() {
	t.root.mu.Lock()
	defer t.root.mu.Unlock()
	t.root.edgeBits = nil
	t.root.edgeLen = 0
	t.root.metadata = nil
	t.root.prefix = nil
	t.root.left = nil
	t.root.right = nil
}
