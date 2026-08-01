// node256.go
package art

import "unsafe"

func NewNode256() *Node256 {
    return &Node256{
        Header: Header{Type: TypeNode256},
    }
}

func (n *Node256) FindChild(b byte) (unsafe.Pointer, bool) {
    child := n.Children[b]
    if child != nil {
        return child, true
    }
    return nil, false
}

func (n *Node256) AddChild(b byte, child unsafe.Pointer) Node {
    if n.Children[b] == nil {
        n.Header.NumChildren++
    }
    n.Children[b] = child
    return n
}

func (n *Node256) RemoveChild(b byte) Node {
    if n.Children[b] != nil {
        n.Children[b] = nil
        n.Header.NumChildren--
        
        // Shrink to Node48 if sparse
        if n.Header.NumChildren < 49 {
            return n.Shrink()
        }
    }
    return n
}

func (n *Node256) IsFull() bool {
    return n.Header.NumChildren >= 255
}

func (n *Node256) IsEmpty() bool {
    return n.Header.NumChildren == 0
}

func (n *Node256) MaxChildren() int { return 256 }
func (n *Node256) MinChildren() int { return 49 }

func (n *Node256) Shrink() Node {
    // Node256 → Node48
    newNode := &Node48{
        Header: n.Header,
        Index:  [256]byte{},
    }
    // Initialize index
    for i := 0; i < 256; i++ {
        newNode.Index[i] = 48
    }
    
    var childIdx uint8
    for i := 0; i < 256; i++ {
        if n.Children[i] != nil {
            newNode.Index[i] = childIdx
            newNode.Children[childIdx] = n.Children[i]
            childIdx++
        }
    }
    newNode.Header.Type = TypeNode48
    newNode.Header.NumChildren = childIdx
    return newNode
}

func (n *Node256) Grow() Node {
    return n
}