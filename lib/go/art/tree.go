// tree.go — Adaptive Radix Tree core (pure Go, lock-protected)
//
// Non-byte-aligned prefix support (/25, /17, etc.)
// ART naturally indexes by full bytes, so a naive implementation would route
// 10.0.0.128/25 and 10.0.0.0/25 to the same child at depth 3 (both have the
// same first 3 bytes).  We handle this as follows:
//
//  1. Insert: route on full bytes up to floor(prefixLen/8).  At the boundary
//     byte (when prefixLen%8 != 0) we mask the byte before using it as the
//     child index, so hosts in different /25 halves land in different slots.
//     We stop descending after that boundary byte — not at the full 4 bytes.
//
//  2. Leaf: stores PrefixLen and MaskedKey so that lookup can verify the
//     significant bits match and ignore host bits.
//
//  3. Lookup (Match): for LPM we follow the tree greedily; whenever we
//     encounter a Leaf mid-traversal we validate it with leaf.matches(ip).
package art

import (
	"net/netip"
	"sync"
	"unsafe"
)

// Tree is the top-level ART structure. It is safe for concurrent use.
type Tree struct {
	root unsafe.Pointer // points to a Node or a LeafNode
	size int
	mu   sync.RWMutex
}

// NewTree returns an empty ART backed by a Node4.
func NewTree() *Tree {
	return &Tree{
		root: unsafe.Pointer(NewNode4()),
	}
}

// InsertPrefix adds or replaces a CIDR prefix entry.
// prefixLen must be the true CIDR length (e.g. 25 for a /25).
func (t *Tree) InsertPrefix(ip netip.Addr, prefixLen uint8, value unsafe.Pointer) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := ip.AsSlice()
	newRoot, added := insertNode(t.root, key, 0, prefixLen, value)
	t.root = newRoot
	if added {
		t.size++
	}
}

// Insert is a convenience wrapper that treats ip as a host address (/32 for
// IPv4, /128 for IPv6) — i.e. exact-match semantics.
func (t *Tree) Insert(ip netip.Addr, value unsafe.Pointer) {
	bits := uint8(ip.BitLen()) // 32 for IPv4, 128 for IPv6
	t.InsertPrefix(ip, bits, value)
}

// insertNode returns (newNodePtr, wasNew).
// depth is the current byte index; prefixLen is the total CIDR length.
func insertNode(ptr unsafe.Pointer, key []byte, depth int, prefixLen uint8, value unsafe.Pointer) (unsafe.Pointer, bool) {
	maxDepth := int(prefixLen+7) / 8 // ceil(prefixLen/8) — how many bytes to traverse

	// Terminal: we have processed all prefix bytes — store a leaf here.
	if depth >= maxDepth {
		leaf := makeLeaf(key, prefixLen, value)
		leafPtr := unsafe.Pointer(leaf)

		// If this slot already holds a leaf, update it in-place.
		if ptr != nil && (*Header)(ptr).Type == TypeLeaf {
			existing := (*LeafNode)(ptr)
			existing.Value = value
			existing.PrefixLen = prefixLen
			existing.MaskedKey = leaf.MaskedKey
			return ptr, false
		}
		return leafPtr, true
	}

	// If the current pointer is a leaf (shorter prefix lives higher), we need
	// to expand it into an internal node so we can insert the new entry below.
	// We do this by building a fresh Node4 and re-inserting BOTH the existing
	// leaf and the new entry from the current depth. Both recursive calls will
	// navigate deeper byte-by-byte until they diverge or reach their maxDepth.
	if ptr != nil && (*Header)(ptr).Type == TypeLeaf {
		existingLeaf := (*LeafNode)(ptr)
		n := Node(NewNode4())

		// Re-insert the existing leaf from this depth.
		b := indexByteForDepth(existingLeaf.MaskedKey[:], depth, existingLeaf.PrefixLen)
		existingMaxDepth := int(existingLeaf.PrefixLen+7) / 8
		if depth >= existingMaxDepth {
			// Existing leaf terminates here — put it under a sentinel child.
			n = n.addChild(b, unsafe.Pointer(existingLeaf))
		} else {
			newExistingChild, _ := insertNode(nil, existingLeaf.MaskedKey[:], depth+1, existingLeaf.PrefixLen, existingLeaf.Value)
			n = n.addChild(b, newExistingChild)
		}

		// Insert the new entry from this depth.
		newB := indexByteForDepth(key, depth, prefixLen)
		newChild, added := insertNode(nil, key, depth+1, prefixLen, value)
		if newB == b {
			// Same byte at this depth — we need to push deeper into the subtree.
			// Replace the child we just added with one that holds both entries.
			childPtr, _ := n.findChild(b)
			combined, added2 := insertNode(childPtr, key, depth+1, prefixLen, value)
			updateChild(n, b, combined)
			return asPtr(n), added2
		}
		n = n.addChild(newB, newChild)
		return asPtr(n), added
	}

	node := nodeToNode(ptr)
	if node == nil {
		node = NewNode4()
	}

	b := indexByteForDepth(key, depth, prefixLen)
	child, found := node.findChild(b)
	if found {
		newChild, added := insertNode(child, key, depth+1, prefixLen, value)
		if newChild != child {
			updateChild(node, b, newChild)
		}
		return asPtr(node), added
	}

	// No child for this key byte — create a leaf directly.
	leaf := makeLeaf(key, prefixLen, value)
	node = node.addChild(b, unsafe.Pointer(leaf))
	return asPtr(node), true
}

func updateChild(n Node, b byte, newChild unsafe.Pointer) {
	switch v := n.(type) {
	case *Node4:
		for i := 0; i < int(v.Header.NumChildren); i++ {
			if v.Keys[i] == b {
				v.Children[i] = newChild
				return
			}
		}
	case *Node16:
		for i := 0; i < int(v.Header.NumChildren); i++ {
			if v.Keys[i] == b {
				v.Children[i] = newChild
				return
			}
		}
	case *Node48:
		idx := v.Index[b]
		if idx != node48Empty {
			v.Children[idx] = newChild
		}
	case *Node256:
		v.Children[b] = newChild
	}
}



// makeLeaf constructs a heap-allocated LeafNode with MaskedKey pre-computed.
func makeLeaf(key []byte, prefixLen uint8, value unsafe.Pointer) *LeafNode {
	leaf := &LeafNode{
		Header:    Header{Type: TypeLeaf},
		Value:     value,
		PrefixLen: prefixLen,
	}
	fullBytes := int(prefixLen) / 8
	remainBits := int(prefixLen) % 8
	for i := 0; i < fullBytes && i < len(key); i++ {
		leaf.MaskedKey[i] = key[i]
	}
	if remainBits > 0 && fullBytes < len(key) {
		mask := byte(0xFF << (8 - remainBits))
		leaf.MaskedKey[fullBytes] = key[fullBytes] & mask
	}
	return leaf
}

// Match returns the stored value for ip, or (nil, false) on miss.
// Performs a simple exact-match at /32 (IPv4) or /128 (IPv6) depth.
func (t *Tree) Match(ip netip.Addr) (unsafe.Pointer, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := ip.AsSlice()
	return searchNode(t.root, key, 0, uint8(ip.BitLen()))
}

func searchNode(ptr unsafe.Pointer, key []byte, depth int, prefixLen uint8) (unsafe.Pointer, bool) {
	if ptr == nil {
		return nil, false
	}

	h := (*Header)(ptr)
	if h.Type == TypeLeaf {
		leaf := (*LeafNode)(ptr)
		// Validate that the significant prefix bits match.
		if leaf.matches(key) {
			return leaf.Value, true
		}
		return nil, false
	}

	// i.e maxdepth is 8 (for IPv6) or 4 (for IPv4)
	maxDepth := int(prefixLen+7) / 8
	if depth >= maxDepth {
		return nil, false
	}

	node := nodeToNode(ptr)
	if node == nil {
		return nil, false
	}

	b := key[depth]
	child, found := node.findChild(b)
	if !found {
		return nil, false
	}
	return searchNode(child, key, depth+1, prefixLen)
}

// Delete removes ip and returns true if it existed.
func (t *Tree) Delete(ip netip.Addr) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := ip.AsSlice()
	bits := uint8(ip.BitLen())
	newRoot, deleted := deleteNode(t.root, key, 0, bits)
	t.root = newRoot
	if deleted {
		t.size--
	}
	return deleted
}

// DeletePrefix deletes a prefix entry with an explicit prefix length.
func (t *Tree) DeletePrefix(ip netip.Addr, prefixLen uint8) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := ip.AsSlice()
	newRoot, deleted := deleteNode(t.root, key, 0, prefixLen)
	t.root = newRoot
	if deleted {
		t.size--
	}
	return deleted
}

func deleteNode(ptr unsafe.Pointer, key []byte, depth int, prefixLen uint8) (unsafe.Pointer, bool) {
	if ptr == nil {
		return nil, false
	}

	maxDepth := int(prefixLen+7) / 8

	h := (*Header)(ptr)
	if h.Type == TypeLeaf {
		leaf := (*LeafNode)(ptr)
		if leaf.PrefixLen == prefixLen && leaf.matches(key) {
			return nil, true
		}
		return ptr, false
	}

	if depth >= maxDepth {
		return ptr, false
	}

	node := nodeToNode(ptr)
	if node == nil {
		return ptr, false
	}

	b := indexByteForDepth(key, depth, prefixLen)
	child, found := node.findChild(b)
	if !found {
		return ptr, false
	}

	newChild, deleted := deleteNode(child, key, depth+1, prefixLen)
	if !deleted {
		return ptr, false
	}

	if newChild == nil {
		node = node.removeChild(b)
	} else if newChild != child {
		updateChild(node, b, newChild)
	}

	if node == nil || node.isEmpty() {
		return nil, true
	}
	return asPtr(node), true
}

// Size returns the number of entries stored in the tree.
func (t *Tree) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.size
}
