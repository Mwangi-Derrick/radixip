// node48.go
package art

import "unsafe"

func NewNode48() *Node48 {
    n := &Node48{
        Header: Header{Type: TypeNode48},
        Index:  [256]byte{},
    }
    // Initialize with empty index
    for i := 0; i < 256; i++ {
        n.Index[i] = 48 // Sentinel
    }
    return n
}

func (n *Node48) FindChild(b byte) (unsafe.Pointer, bool) {
    idx := n.Index[b]
    if idx < 48 {
        return n.Children[idx], true
    }
    return nil, false
}

func (n *Node48) AddChild(b byte, child unsafe.Pointer) Node {
    if n.IsFull() {
        // Grow to Node256
        return n.Grow().AddChild(b, child)
    }
    
    idx := n.Header.NumChildren
    n.Index[b] = idx
    n.Children[idx] = child
    n.Header.NumChildren++
    
    // Grow if dense
    if n.Header.NumChildren >= 45 {
        return n.Grow()
    }
    return n
}

func (n *Node48) RemoveChild(b byte) Node {
    idx := n.Index[b]
    if idx >= 48 {
        return n
    }
    
    // Remove from children
    n.Children[idx] = nil
    n.Index[b] = 48
    n.Header.NumChildren--
    
    // Shrink to Node16 if sparse
    if n.Header.NumChildren < 17 {
        return n.Shrink()
    }
    return n
}

func (n *Node48) IsFull() bool {
    return n.Header.NumChildren >= 48
}

func (n *Node48) MaxChildren() int { return 48 }
func (n *Node48) MinChildren() int { return 17 }

func (n *Node48) Grow() Node {
    newNode := &Node256{
        Header: n.Header,
    }
    
    for i := 0; i < 256; i++ {
        idx := n.Index[i]
        if idx < 48 {
            newNode.Children[i] = n.Children[idx]
        }
    }
    newNode.Header.Type = TypeNode256
    return newNode
}

func (n *Node48) Shrink() Node {
    // Node48 → Node16
    newNode := &Node16{
        Header: n.Header,
    }
    var childIdx int
    for i := 0; i < 256; i++ {
        idx := n.Index[i]
        if idx < 48 && n.Children[idx] != nil {
            newNode.Keys[childIdx] = byte(i)
            newNode.Children[childIdx] = n.Children[idx]
            childIdx++
        }
    }
    newNode.Header.NumChildren = uint8(childIdx)
    newNode.Header.Type = TypeNode16
    return newNode
}