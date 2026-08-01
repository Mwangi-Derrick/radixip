// node16.go
package art

import "unsafe"

func NewNode16() *Node16 {
    return &Node16{
        Header: Header{Type: TypeNode16},
    }
}

// FindChild with optimized search
func (n *Node16) FindChild(b byte) (unsafe.Pointer, bool) {
    // Use SIMD if available, otherwise linear
    if useSIMD {
        idx := simdFindKey(&n.Keys, b, n.Header.NumChildren)
        if idx >= 0 {
            return n.Children[idx], true
        }
        return nil, false
    }
    
    // Fallback linear search (16 entries)
    for i := 0; i < int(n.Header.NumChildren); i++ {
        if n.Keys[i] == b {
            return n.Children[i], true
        }
    }
    return nil, false
}

func (n *Node16) AddChild(b byte, child unsafe.Pointer) Node {
    if n.IsFull() {
        // Grow to Node48
        newNode := n.Grow()
        newNode.AddChild(b, child)
		return newNode
    }
    
    idx := n.Header.NumChildren
    n.Keys[idx] = b
    n.Children[idx] = child
    n.Header.NumChildren++
    
    // Check if we should grow (density threshold)
    if n.Header.NumChildren >= 13 {
        return n.Grow()
    }
    return n
}

func (n *Node16) RemoveChild(b byte) Node {
    for i := 0; i < int(n.Header.NumChildren); i++ {
        if n.Keys[i] == b {
            for j := i; j < int(n.Header.NumChildren)-1; j++ {
                n.Keys[j] = n.Keys[j+1]
                n.Children[j] = n.Children[j+1]
            }
            n.Header.NumChildren--
            
            // Shrink to Node4 if sparse
            if n.Header.NumChildren < 4 {
                return n.Shrink()
            }
            return n
        }
    }
    return n
}

func (n *Node16) IsFull() bool {
    return n.Header.NumChildren >= 16
}

func (n *Node16) MaxChildren() int { return 16 }
func (n *Node16) MinChildren() int { return 4 }

func (n *Node16) Grow() Node {
    // Node16 → Node48
    newNode := &Node48{
        Header: n.Header,
        Index:  [256]byte{},
    }
    // Initialize index with 48
    for i := 0; i < 256; i++ {
        newNode.Index[i] = 48
    }
    
    for i := 0; i < int(n.Header.NumChildren); i++ {
        b := n.Keys[i]
        newNode.Index[b] = byte(i)
        newNode.Children[i] = n.Children[i]
    }
    newNode.Header.Type = TypeNode48
    return newNode
}

func (n *Node16) Shrink() Node {
    // Node16 → Node4
    newNode := &Node4{
        Header: n.Header,
    }
    for i := 0; i < int(n.Header.NumChildren); i++ {
        newNode.Keys[i] = n.Keys[i]
        newNode.Children[i] = n.Children[i]
    }
    newNode.Header.Type = TypeNode4
    return newNode
}