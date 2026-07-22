package radixip

import (
	"net"
	"sync"

	"github.com/Mwangi-Derrick/radixip/lib/go/node"
)

//
// UNCOMPRESSED TREE
//
// Each prefix is a full path from root to leaf.
// O(P) where P = max prefix length (128 for IPv6), regardless of branching.
// Best for heavy modification workloads where tree shape changes often.

type UncompressedTree struct {
	root        RadixNode
	nodeBuilder *node.NodeBuilder
}

func NewUncompressedTree(nodeVariant NodeVariant) *UncompressedTree {
	builder := node.NewNodeBuilder(nodeVariant)
	return &UncompressedTree{
		root:        builder.Build(),
		nodeBuilder: builder,
	}
}

func (t *UncompressedTree) Insert(prefix IpNetwork, metadata Metadata) (bool, error) {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := t.root

	for depth := 0; depth < prefixLen; depth++ {
		bit := t.getBit(ip, depth)
		var next RadixNode
		if bit == 0 {
			next = current.Left()
		} else {
			next = current.Right()
		}

		if next != nil {
			current = next
		} else {
			newNode := t.nodeBuilder.Build()
			if bit == 0 {
				current.SetLeft(newNode)
			} else {
				current.SetRight(newNode)
			}
			current = newNode
		}
	}

	isNew := current.Metadata() == nil

	netPrefix := net.IPNet{IP: prefix.IP, Mask: prefix.Mask}
	current.SetPrefix(&netPrefix)
	current.SetMetadata(&metadata)

	return isNew, nil
}

func (t *UncompressedTree) Lookup(ip *net.IP) *Metadata {
	var bestMatch *Metadata
	current := t.root
	depth := 0

	for current != nil {
		if p := current.Prefix(); p != nil {
			if p.Contains(*ip) {
				bestMatch = current.Metadata()
			}
		}

		bit := t.getBit(*ip, depth)
		if bit == 0 {
			current = current.Left()
		} else {
			current = current.Right()
		}
		depth++
	}

	return bestMatch
}

func (t *UncompressedTree) Remove(prefix *IpNetwork) *Metadata {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := t.root
	for depth := 0; depth < prefixLen; depth++ {
		bit := t.getBit(ip, depth)
		if bit == 0 {
			current = current.Left()
		} else {
			current = current.Right()
		}
		if current == nil {
			return nil
		}
	}

	removed := current.Metadata()
	if removed != nil {
		current.ClearMetadata()
	}

	return removed
}

func (t *UncompressedTree) Contains(prefix *IpNetwork) bool {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := t.root
	for depth := 0; depth < prefixLen; depth++ {
		bit := t.getBit(ip, depth)
		if bit == 0 {
			current = current.Left()
		} else {
			current = current.Right()
		}
		if current == nil {
			return false
		}
	}
	return current.Metadata() != nil
}

func (t *UncompressedTree) Clear() {
	t.root.SetLeft(nil)
	t.root.SetRight(nil)
}

// longestPrefixMatch is now implemented directly in UncompressedTree and CompressedTree Lookups

// t.getBit returns the bit at the specified position from an IP
func (t *UncompressedTree) getBit(ip net.IP, bitPos int) int {
	// Convert IP to byte slice
	ipBytes := ip.To4()
	if ipBytes == nil {
		ipBytes = ip.To16()
	}

	if ipBytes == nil {
		return 0
	}

	// Find the byte and bit within the byte
	byteIdx := bitPos / 8
	// we count bits from left to right( most significant to least significant )
	bitIdx := 7 - (bitPos % 8) // Most significant bit first

	if byteIdx >= len(ipBytes) {
		return 0
	}

	if (ipBytes[byteIdx]>>bitIdx)&1 == 1 {
		return 1
	}
	return 0
}

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
	metadata *Metadata
	prefix   *net.IPNet
	left     *compressedNode
	right    *compressedNode
}

// CompressedTree is a Patricia / compressed radix trie.
// It is safe for concurrent use via per-node mutexes (fine-grained locking).
type CompressedTree struct {
	root *compressedNode
}

func NewCompressedTree(_ NodeVariant) *CompressedTree {
	return &CompressedTree{root: &compressedNode{}}
}

func (t *CompressedTree) insertNode(n *compressedNode, key []byte, keyLen, depth int, prefix *net.IPNet, meta *Metadata) bool {
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

func (t *CompressedTree) lookupNode(n *compressedNode, key []byte, depth int) *Metadata {
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

func (t *CompressedTree) removeNode(n *compressedNode, key []byte, keyLen, depth int) *Metadata {
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

func (t *CompressedTree) Insert(prefix IpNetwork, metadata Metadata) (bool, error) {
	key := ipToBytes(prefix.IP)
	ones, _ := prefix.Mask.Size()
	netPrefix := net.IPNet{IP: prefix.IP, Mask: prefix.Mask}
	return t.insertNode(t.root, key, ones, 0, &netPrefix, &metadata), nil
}

func (t *CompressedTree) Lookup(ip *net.IP) *Metadata {
	key := ipToBytes(*ip)
	return t.lookupNode(t.root, key, 0)
}

func (t *CompressedTree) Remove(prefix *IpNetwork) *Metadata {
	key := ipToBytes(prefix.IP)
	ones, _ := prefix.Mask.Size()
	return t.removeNode(t.root, key, ones, 0)
}

func (t *CompressedTree) Contains(prefix *IpNetwork) bool {
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

func getBitFromBytes(b []byte, pos int) uint8 {
	byteIdx := pos / 8
	bitIdx := 7 - (pos % 8)
	if byteIdx >= len(b) {
		return 0
	}
	return (b[byteIdx] >> uint(bitIdx)) & 1
}

func extractBits(b []byte, start, length int) []byte {
	byteCount := (length + 7) / 8
	out := make([]byte, byteCount)
	for i := 0; i < length; i++ {
		bit := getBitFromBytes(b, start+i)
		byteI := i / 8
		bitI := 7 - (i % 8)
		out[byteI] |= bit << uint(bitI)
	}
	return out
}

func commonPrefixLen(a []byte, aLen int, bb []byte, bLen int) int {
	max := aLen
	if bLen < max {
		max = bLen
	}
	for i := 0; i < max; i++ {
		if getBitFromBytes(a, i) != getBitFromBytes(bb, i) {
			return i
		}
	}
	return max
}

func ipToBytes(ip net.IP) []byte {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip.To16()
}
