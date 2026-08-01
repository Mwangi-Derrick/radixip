// tree.go — Adaptive Radix Tree core (pure Go, lock-protected)
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

// Insert adds or replaces the value for ip.
func (t *Tree) Insert(ip netip.Addr, value unsafe.Pointer) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := ip.AsSlice()
	newRoot, added := insertNode(t.root, key, 0, value)
	t.root = newRoot
	if added {
		t.size++
	}
}

// insertNode returns (newNodePtr, wasNew).
func insertNode(ptr unsafe.Pointer, key []byte, depth int, value unsafe.Pointer) (unsafe.Pointer, bool) {
	// Terminal: store a leaf at this depth.
	if depth >= len(key) {
		wasNew := ptr == nil || (*Header)(ptr).Type == TypeNode4 // rough heuristic
		leaf := &LeafNode{Value: value}
		return unsafe.Pointer(leaf), wasNew
	}

	// If the current pointer is a leaf, we have arrived at the end of a
	// previously-stored shorter prefix — replace it and descend.
	if ptr != nil && (*Header)(ptr).Type == TypeLeaf {
		// Overwrite existing leaf
		(*LeafNode)(ptr).Value = value
		return ptr, false
	}

	node := nodeToNode(ptr)
	if node == nil {
		// First time we descend into this slot — allocate a Node4.
		node = NewNode4()
	}

	b := key[depth]
	child, found := node.findChild(b)
	if found {
		newChild, added := insertNode(child, key, depth+1, value)
		if newChild != child {
			node = node.addChild(b, newChild)
		}
		return asPtr(node), added
	}

	// No child for this byte — create a leaf and attach it.
	leaf := unsafe.Pointer(&LeafNode{Value: value})
	node = node.addChild(b, leaf)
	return asPtr(node), true
}

// Match returns the stored value for ip, or (nil, false) on miss.
func (t *Tree) Match(ip netip.Addr) (unsafe.Pointer, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := ip.AsSlice()
	return searchNode(t.root, key, 0)
}

func searchNode(ptr unsafe.Pointer, key []byte, depth int) (unsafe.Pointer, bool) {
	if ptr == nil {
		return nil, false
	}

	h := (*Header)(ptr)
	if h.Type == TypeLeaf {
		// Only a match if we consumed all key bytes
		if depth >= len(key) {
			return (*LeafNode)(ptr).Value, true
		}
		return nil, false
	}

	if depth >= len(key) {
		return nil, false
	}

	node := nodeToNode(ptr)
	if node == nil {
		return nil, false
	}

	child, found := node.findChild(key[depth])
	if !found {
		return nil, false
	}
	return searchNode(child, key, depth+1)
}

// Delete removes ip and returns true if it existed.
func (t *Tree) Delete(ip netip.Addr) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := ip.AsSlice()
	newRoot, deleted := deleteNode(t.root, key, 0)
	t.root = newRoot
	if deleted {
		t.size--
	}
	return deleted
}

func deleteNode(ptr unsafe.Pointer, key []byte, depth int) (unsafe.Pointer, bool) {
	if ptr == nil {
		return nil, false
	}

	h := (*Header)(ptr)
	if h.Type == TypeLeaf {
		if depth >= len(key) {
			return nil, true // remove the leaf
		}
		return ptr, false
	}

	if depth >= len(key) {
		return ptr, false
	}

	node := nodeToNode(ptr)
	if node == nil {
		return ptr, false
	}

	b := key[depth]
	child, found := node.findChild(b)
	if !found {
		return ptr, false
	}

	newChild, deleted := deleteNode(child, key, depth+1)
	if !deleted {
		return ptr, false
	}

	if newChild == nil {
		node = node.removeChild(b)
	} else if newChild != child {
		node = node.addChild(b, newChild)
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