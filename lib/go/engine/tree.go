package radixip

import (
	"fmt"
	"net"
)

//
// UNCOMPRESSED TREE
//
// Each prefix is a full path from root to leaf.
// O(P) where P = max prefix length (128 for IPv6), regardless of branching.
// Best for heavy modification workloads where tree shape changes often.

type UncompressedTree struct {
	root        Node
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
	ip := prefix.IP               //gets the ip address
	ones, _ := prefix.Mask.Size() // gets the prefix of the network
	prefixLen := ones             // sets the prefix length or the number of bits we process
	if t.root == nil {
		t.root = t.nodeBuilder.Build()
		if t.root == nil {
			return false, fmt.Errorf("failed to create root node")
		}
	}

	current := t.root //starts at the root

	// only loop the number of bits in the mask
	for depth := 0; depth < prefixLen; depth++ {
		bit := t.getBit(ip, depth) // gets the bit at the current depth (0 or 1)
		var next Node
		if bit == 0 {
			if current == nil {
				// Handle nil current
				current = t.nodeBuilder.Build()
				t.root = current
			}
			// if bit is 0, go left
			next = current.Left()
		} else {
			if current == nil {
				// Handle nil current
				current = t.nodeBuilder.Build()
				t.root = current
			}
			// if bit is 1, go right
			next = current.Right()
		}

		// if the next node is not nil, just continue
		if next != nil {
			// this means we traverse node down the tree
			// if there is a node no need to create it
			current = next // moves the current node down to the next node
		} else {
			// if the next node is nil, we create it
			newNode := t.nodeBuilder.Build()
			if bit == 0 {
				//if the bit was 0 and node was nil, we set the left child
				current.SetLeft(newNode)
			} else {
				//if the bit was 1 and node was nil, we set the right child
				current.SetRight(newNode)
			}
			// moves the current node down to the newly created node
			current = newNode
		}
	}

	// checks if the current node is new
	isNew := current.Metadata() == nil

	// sets the prefix of the current node since we have reached the end of our prefix
	netPrefix := new(net.IPNet)
	*netPrefix = net.IPNet{IP: prefix.IP, Mask: prefix.Mask}
	current.SetPrefix(netPrefix)
	// sets the metadata of the current node
	newMeta := new(Metadata)
	*newMeta = metadata
	current.SetMetadata(newMeta)

	return isNew, nil
}

func (t *UncompressedTree) Lookup(ip *net.IP) *Metadata {
	var bestMatch *Metadata
	if t.root == nil {
		return nil
	}
	current := t.root
	depth := 0

	// the loop continues as long as the current node is not nil
	for current != nil {
		// check if current has prefix
		if p := current.Prefix(); p != nil {
			// check if the prefix contains the ip
			if p.Contains(*ip) {
				// if yes, bestmatch is the metadata of the current node
				bestMatch = current.Metadata()
			}
		}

		// gets the bit at the current depth (0 or 1)
		bit := t.getBit(*ip, depth)
		if bit == 0 {
			// if bit is 0, go left
			current = current.Left()
		} else {
			// if bit is 1, go right
			current = current.Right()
		}
		depth++
	}

	return bestMatch
}

func (t *UncompressedTree) Remove(prefix *IpNetwork) *Metadata {
	if t.root == nil {
		return nil
	}
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
	if t.root == nil {
		return false
	}
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
	if t.root != nil {
		t.root.SetLeft(nil)
		t.root.SetRight(nil)
	}
}

// longestPrefixMatch is now implemented directly in UncompressedTree and CompressedTree Lookups
// uses bit masking to get the 1 bit if it matches otherwise it retruns 0
// t.getBit returns the bit at the specified position from an IP
func (t *UncompressedTree) getBit(ip net.IP, bitPos int) int {
	// Convert IP to byte slice
	// For IPv4 192.168.1.0, ipBytes = [192, 168, 1, 0]
	ipBytes := ip.To4() // [192, 168, 1, 0]
	if ipBytes == nil {
		//for ipv6
		// ipBytes = ip.To16() // [fd00::1] -> [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1]
		ipBytes = ip.To16()
	}

	if ipBytes == nil {
		return 0
	}

	// Find the byte and bit within the byte
	byteIdx := bitPos / 8 // Which byte?
	// bitPos 0-7 → byte 0 (192)
	// bitPos 8-15 → byte 1 (168)
	// we count bits from left to right( most significant to least significant )
	//index is from (0-7) right-most to left-most
	bitIdx := 7 - (bitPos % 8) // Most significant bit first

	if byteIdx >= len(ipBytes) {
		return 0
	}

	//this ensures that we mask the bit with padded 0s
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
	root        Node
	nodeBuilder *NodeBuilder
}

func NewCompressedTree(variant NodeVariant) *CompressedTree {
	compressedVariant := variant
	switch variant {
	case NormalTrieNode, NormalRadixNode:
		compressedVariant = NormalRadixNode
	case AtomicTrieNode, AtomicRadixNode:
		compressedVariant = AtomicRadixNode
	case PaddedTrieNode, PaddedRadixNode:
		compressedVariant = PaddedRadixNode
	case LockFreeTrieNode, LockFreeRadixNode:
		compressedVariant = LockFreeRadixNode
	}

	builder := NewNodeBuilder(compressedVariant)
	return &CompressedTree{
		root:        builder.Build(),
		nodeBuilder: builder,
	}
}

func (t *CompressedTree) insertNode(n Node, key []byte, keyLen, depth int, prefix *net.IPNet, meta *Metadata) bool {
	// edgeBits are ALL the bits stored in this node
	// (the compressed path from parent to this node)
	edgeBits := n.EdgeBits()

	// edgeLen is how many bits are in this edge
	edgeLen := n.EdgeLen()

	// get the remaining bits
	// depth is the length of the prefix already processed
	// keyLen is the total length of the prefix
	// i.e depth is the bits that matter or mask bits
	// remaining is the number of bits left to process
	// depth tells us: "We've already processed 'depth' bits"
	// So the next bit to process is at position 'depth'
	remaining := keyLen - depth // how many bits are remaining to process
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

	shared := commonPrefixLenOffset(edgeBits, 0, edgeLen, key, depth, remaining)

	// Exact match
	if shared == edgeLen && shared == remaining {
		isNew := n.Metadata() == nil
		n.SetMetadata(meta)
		n.SetPrefix(prefix)
		return isNew
	}

	if shared < edgeLen {
		// Split node
		pivotBit := getBitFromBytes(edgeBits, shared)
		child := t.nodeBuilder.Build()
		child.SetEdge(extractBits(edgeBits, shared+1, edgeLen-shared-1), edgeLen-shared-1)
		child.SetMetadata(n.Metadata())
		child.SetPrefix(n.Prefix())
		child.SetLeft(n.Left())
		child.SetRight(n.Right())

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

		newBit := getBitFromBytes(key, depth+shared)
		newLeafEdge := extractBits(key, depth+shared+1, remaining-shared-1)
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

	// Descend case
	nextBit := getBitFromBytes(key, depth+shared)
	var child Node
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

func (t *CompressedTree) lookupNode(n Node, key []byte, depth int) *Metadata {
	if n == nil {
		return nil
	}
	edgeBits := n.EdgeBits()
	edgeLen := n.EdgeLen()

	remaining := len(key)*8 - depth
	if remaining < 0 {
		remaining = 0
	}
	shared := commonPrefixLenOffset(edgeBits, 0, edgeLen, key, depth, remaining)

	if shared < edgeLen {
		return nil
	}

	best := n.Metadata()
	newDepth := depth + shared
	var nextChild Node
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

func (t *CompressedTree) removeNode(n Node, key []byte, keyLen, depth int) *Metadata {
	if n == nil {
		return nil
	}
	edgeBits := n.EdgeBits()
	edgeLen := n.EdgeLen()

	remaining := keyLen - depth
	if remaining < 0 {
		remaining = 0
	}
	shared := commonPrefixLenOffset(edgeBits, 0, edgeLen, key, depth, remaining)

	if shared < edgeLen {
		return nil
	}

	if shared == remaining {
		removed := n.Metadata()
		n.ClearMetadata()
		n.SetPrefix(nil)
		return removed
	}

	nextBit := getBitFromBytes(key, depth+shared)
	var child Node
	if nextBit == 0 {
		child = n.Left()
	} else {
		child = n.Right()
	}
	return t.removeNode(child, key, keyLen, depth+shared+1)
}

func (t *CompressedTree) containsNode(n Node, key []byte, keyLen, depth int) bool {
	if n == nil {
		return false
	}
	edgeBits := n.EdgeBits()
	edgeLen := n.EdgeLen()

	remaining := keyLen - depth
	if remaining < 0 {
		remaining = 0
	}
	shared := commonPrefixLenOffset(edgeBits, 0, edgeLen, key, depth, remaining)

	if shared < edgeLen {
		return false
	}
	if shared == remaining {
		return n.Metadata() != nil
	}
	nextBit := getBitFromBytes(key, depth+shared)
	var child Node
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
	netPrefix := new(net.IPNet)
	*netPrefix = net.IPNet{IP: prefix.IP, Mask: prefix.Mask}
	newMeta := new(Metadata)
	*newMeta = metadata
	return t.insertNode(t.root, key, ones, 0, netPrefix, newMeta), nil
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

func commonPrefixLenOffset(a []byte, aOffset, aLen int, b []byte, bOffset, bLen int) int {
	max := aLen
	if bLen < max {
		max = bLen
	}
	for i := 0; i < max; i++ {
		if getBitFromBytes(a, aOffset+i) != getBitFromBytes(b, bOffset+i) {
			return i
		}
	}
	return max
}

func commonPrefixLen(a []byte, aLen int, bb []byte, bLen int) int {

	// a is the edge of fisrt bits
	// bb is the key of what we want to lookup or insert
	max := aLen
	if bLen < max {
		max = bLen
	}
	// Compare bits one by one
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
