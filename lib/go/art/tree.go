// tree.go
package art

import (
    "net/netip"
    "sync"
    "unsafe"
)

type Tree struct {
    root unsafe.Pointer // *Node
    size int
    mu   sync.RWMutex
}

func NewTree() *Tree {
    // Start with empty Node4
    root := unsafe.Pointer(NewNode4())
    return &Tree{
        root: root,
        size: 0,
    }
}

func (t *Tree) Insert(ip netip.Addr, value unsafe.Pointer) {
    t.mu.Lock()
    defer t.mu.Unlock()
    
    ipBytes := ip.AsSlice()
    t.root = t.insertNode(t.root, ipBytes, 0, value)
    t.size++
}

func (t *Tree) insertNode(nodePtr unsafe.Pointer, key []byte, depth int, value unsafe.Pointer) unsafe.Pointer {
    // Check if we're at a leaf
    if depth >= len(key) {
        // Create leaf node
        leaf := &LeafNode{Value: value}
        return unsafe.Pointer(leaf)
    }
    
    node := nodeToNode(nodePtr)
    if node == nil {
        // Create Node4
        n := NewNode4()
        node = n
    }
    
    b := key[depth]
    child, found := node.FindChild(b)
    
    if found {
        // Recurse
        newChild := t.insertNode(child, key, depth+1, value)
        node = node.AddChild(b, newChild)
        return unsafe.Pointer(node.(interface{}) )
    }
    
    // No child, insert new leaf
    leaf := unsafe.Pointer(&LeafNode{Value: value})
    
    // Check if node is full and grow if needed
    if node.IsFull() {
        node = node.Grow()
    }
    
    node = node.AddChild(b, leaf)
    return unsafe.Pointer(node.(interface{}))
}

func (t *Tree) Match(ip netip.Addr) (unsafe.Pointer, bool) {
    t.mu.RLock()
    defer t.mu.RUnlock()
    
    ipBytes := ip.AsSlice()
    return t.search(t.root, ipBytes, 0)
}

func (t *Tree) search(nodePtr unsafe.Pointer, key []byte, depth int) (unsafe.Pointer, bool) {
    if nodePtr == nil {
        return nil, false
    }
    
    // Check if leaf
    if depth >= len(key) {
        leaf := (*LeafNode)(nodePtr)
        return leaf.Value, true
    }
    
    node := nodeToNode(nodePtr)
    if node == nil {
        return nil, false
    }
    
    b := key[depth]
    child, found := node.FindChild(b)
    if !found {
        return nil, false
    }
    
    return t.search(child, key, depth+1)
}

func (t *Tree) Delete(ip netip.Addr) bool {
    t.mu.Lock()
    defer t.mu.Unlock()
    
    ipBytes := ip.AsSlice()
    var deleted bool
    t.root = t.deleteNode(t.root, ipBytes, 0, &deleted)
    if deleted {
        t.size--
        return true
    }
    return false
}

func (t *Tree) deleteNode(nodePtr unsafe.Pointer, key []byte, depth int, deleted *bool) unsafe.Pointer {
    if nodePtr == nil {
        return nil
    }
    
    if depth >= len(key) {
        *deleted = true
        return nil // Remove leaf
    }
    
    node := nodeToNode(nodePtr)
    if node == nil {
        return nil
    }
    
    b := key[depth]
    child, found := node.FindChild(b)
    if !found {
        return nodePtr
    }
    
    // Recursively delete
    newChild := t.deleteNode(child, key, depth+1, deleted)
    
    if *deleted {
        // Remove child from node
        node = node.RemoveChild(b)
        
        // Shrink if needed
        if node != nil && !node.IsEmpty() {
            if node.MaxChildren() > 4 && node.Header.NumChildren < uint8(node.MinChildren()) {
                node = node.(interface{ Shrink() Node }).Shrink()
            }
        }
    }
    
    if node == nil || node.IsEmpty() {
        return nil
    }
    return unsafe.Pointer(node.(interface{}))
}

func (t *Tree) Size() int {
    t.mu.RLock()
    defer t.mu.RUnlock()
    return t.size
}

// Helper to convert unsafe.Pointer to Node interface
func nodeToNode(p unsafe.Pointer) Node {
    if p == nil {
        return nil
    }
    
    // Check the header type
    header := (*Header)(p)
    switch header.Type {
    case Node4:
        return (*Node4)(p)
    case Node16:
        return (*Node16)(p)
    case Node48:
        return (*Node48)(p)
    case Node256:
        return (*Node256)(p)
    default:
        return nil
    }
}