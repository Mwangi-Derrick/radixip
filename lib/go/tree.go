package radixip

import (
	"net"
	"sync"
)

//
// UNCOMPRESSED TREE
//
// Each prefix is a full path from root to leaf.
// O(P) where P = max prefix length (128 for IPv6), regardless of branching.
// Best for heavy modification workloads where tree shape changes often.

type UncompressedTree struct {
	root        RadixNode
	nodeBuilder *NodeBuilder
}

func NewUncompressedTree(nodeVariant NodeVariant) *UncompressedTree {
	builder := NewNodeBuilder(nodeVariant)
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

// CompressedTree is a Patricia / compressed radix trie.
type CompressedTree struct {
	root        RadixNode
	nodeBuilder *NodeBuilder
}

func NewCompressedTree(variant NodeVariant) *CompressedTree {
	compressedVariant := variant
	switch variant {
	case NodeNormal, NodeCompressedNormal:
		compressedVariant = NodeCompressedNormal
	case NodeAtomic, NodeCompressedAtomic:
		compressedVariant = NodeCompressedAtomic
	case NodePadded, NodeCompressedPadded:
		compressedVariant = NodeCompressedPadded
	case NodeLockFree, NodeCompressedLockFree:
		compressedVariant = NodeCompressedLockFree
	}

	builder := NewNodeBuilder(compressedVariant)
	return &CompressedTree{
		root:        builder.Build(),
		nodeBuilder: builder,
	}
}

func (t *CompressedTree) insertNode(n RadixNode, key []byte, keyLen, depth int, prefix *net.IPNet, meta *Metadata) bool {
	edgeBits := n.EdgeBits()
	edgeLen := n.EdgeLen()

	remaining := keyLen - depth
	if remaining < 0 {
		remaining = 0
	}

	// Empty node — store directly
	if edgeLen == 0 && n.Metadata() == nil && n.Left() == nil && n.Right() == nil {
		n.SetEdge(extractBits(key, depth, remaining), remaining)
		n.SetPrefix(prefix)
		isNew := n.Metadata() == nil
		n.SetMetadata(meta)
		return isNew
	}

	keyRem := extractBits(key, depth, remaining)
	shared := commonPrefixLen(edgeBits, edgeLen, keyRem, remaining)

	// Exact match
	if shared == edgeLen && shared == remaining {
		isNew := n.Metadata() == nil
		n.SetMetadata(meta)
		n.SetPrefix(prefix)
		return isNew
	}

	// Partial match — split
	if shared < edgeLen {
		pivotBit := getBitFromBytes(edgeBits, shared)
		// New child carries current edge's remainder
		child := t.nodeBuilder.Build()
		child.SetEdge(extractBits(edgeBits, shared+1, edgeLen-shared-1), edgeLen-shared-1)
		child.SetMetadata(n.Metadata())
		child.SetPrefix(n.Prefix())
		child.SetLeft(n.Left())
		child.SetRight(n.Right())

		// Trim current node to shared prefix
		n.SetEdge(extractBits(edgeBits, 0, shared), shared)
		n.ClearMetadata()
		n.SetPrefix(nil)
		n.SetLeft(nil)
		n.SetRight(nil)

		if pivotBit == 0 {
			n.SetLeft(child)
		} else {
			n.SetRight(child)
		}

		if shared == remaining {
			n.SetMetadata(meta)
			n.SetPrefix(prefix)
			return true
		}

		newBit := getBitFromBytes(keyRem, shared)
		newLeafEdge := extractBits(keyRem, shared+1, remaining-shared-1)
		newLeaf := t.nodeBuilder.Build()
		newLeaf.SetEdge(newLeafEdge, remaining-shared-1)
		newLeaf.SetMetadata(meta)
		newLeaf.SetPrefix(prefix)

		if newBit == 0 {
			n.SetLeft(newLeaf)
		} else {
			n.SetRight(newLeaf)
		}
		return true
	}

	// Descend
	nextBit := getBitFromBytes(keyRem, shared)
	var child RadixNode
	if nextBit == 0 {
		child = n.Left()
	} else {
		child = n.Right()
	}

	if child == nil {
		newDepth := depth + shared + 1
		newRemaining := keyLen - newDepth
		if newRemaining < 0 {
			newRemaining = 0
		}
		newLeaf := t.nodeBuilder.Build()
		newLeaf.SetEdge(extractBits(key, newDepth, newRemaining), newRemaining)
		newLeaf.SetMetadata(meta)
		newLeaf.SetPrefix(prefix)

		if nextBit == 0 {
			n.SetLeft(newLeaf)
		} else {
			n.SetRight(newLeaf)
		}
		return true
	}

	return t.insertNode(child, key, keyLen, depth+shared+1, prefix, meta)
}

func (t *CompressedTree) lookupNode(n RadixNode, key []byte, depth int) *Metadata {
	if n == nil {
		return nil
	}
	edgeBits := n.EdgeBits()
	edgeLen := n.EdgeLen()

	remaining := len(key)*8 - depth
	if remaining < 0 {
		remaining = 0
	}
	keyRem := extractBits(key, depth, remaining)
	shared := commonPrefixLen(edgeBits, edgeLen, keyRem, remaining)

	if shared < edgeLen {
		return nil
	}

	best := n.Metadata()
	newDepth := depth + shared
	var nextChild RadixNode
	if newDepth < len(key)*8 {
		nextBit := getBitFromBytes(key, newDepth)
		if nextBit == 0 {
			nextChild = n.Left()
		} else {
			nextChild = n.Right()
		}
	}

	if nextChild != nil {
		if deeper := t.lookupNode(nextChild, key, newDepth+1); deeper != nil {
			best = deeper
		}
	}
	return best
}

func (t *CompressedTree) removeNode(n RadixNode, key []byte, keyLen, depth int) *Metadata {
	if n == nil {
		return nil
	}
	edgeBits := n.EdgeBits()
	edgeLen := n.EdgeLen()

	remaining := keyLen - depth
	if remaining < 0 {
		remaining = 0
	}
	keyRem := extractBits(key, depth, remaining)
	shared := commonPrefixLen(edgeBits, edgeLen, keyRem, remaining)

	if shared < edgeLen {
		return nil
	}

	if shared == remaining {
		removed := n.Metadata()
		n.ClearMetadata()
		n.SetPrefix(nil)
		return removed
	}

	nextBit := getBitFromBytes(keyRem, shared)
	var child RadixNode
	if nextBit == 0 {
		child = n.Left()
	} else {
		child = n.Right()
	}
	return t.removeNode(child, key, keyLen, depth+shared+1)
}

func (t *CompressedTree) containsNode(n RadixNode, key []byte, keyLen, depth int) bool {
	if n == nil {
		return false
	}
	edgeBits := n.EdgeBits()
	edgeLen := n.EdgeLen()

	remaining := keyLen - depth
	if remaining < 0 {
		remaining = 0
	}
	keyRem := extractBits(key, depth, remaining)
	shared := commonPrefixLen(edgeBits, edgeLen, keyRem, remaining)

	if shared < edgeLen {
		return false
	}
	if shared == remaining {
		return n.Metadata() != nil
	}
	nextBit := getBitFromBytes(keyRem, shared)
	var child RadixNode
	if nextBit == 0 {
		child = n.Left()
	} else {
		child = n.Right()
	}
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
	t.root = t.nodeBuilder.Build()
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

