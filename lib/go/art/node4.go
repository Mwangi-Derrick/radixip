// node4.go
package art

import "unsafe"

func NewNode4() *Node4 {
    return &Node4{
        Header: Header{Type: TypeNode4},
        Keys:   [4]byte{0, 0, 0, 0},
        Children: [4]unsafe.Pointer{nil, nil, nil, nil},
    }
}

// FindChild with linear search (only 4 entries)
func (n *Node4) FindChild(b byte) (unsafe.Pointer, bool) {
    for i := 0; i < int(n.Header.NumChildren); i++ {
        if n.Keys[i] == b {
            return n.Children[i], true
        }
    }
    return nil, false
}

func (n *Node4) AddChild(b byte, child unsafe.Pointer) Node {
    if n.IsFull() {
        // Grow to Node16
        newNode := n.Grow()
        // Re-add the child to the new node
        newNode.AddChild(b, child)
        return newNode
    }
    
    idx := n.Header.NumChildren
    n.Keys[idx] = b
    n.Children[idx] = child
    n.Header.NumChildren++
    return n
}

func (n *Node4) RemoveChild(b byte) Node {
    for i := 0; i < int(n.Header.NumChildren); i++ {
        if n.Keys[i] == b {
            // Shift left
            for j := i; j < int(n.Header.NumChildren)-1; j++ {
                n.Keys[j] = n.Keys[j+1]
                n.Children[j] = n.Children[j+1]
            }
            n.Header.NumChildren--
            
            // Check if we need to shrink
            if n.Header.NumChildren < 1 {
                // Convert to leaf or handle empty
            }
            return n
        }
    }
    return n
}

func (n *Node4) IsFull() bool {
    return n.Header.NumChildren >= 4
}

func (n *Node4) IsEmpty() bool {
    return n.Header.NumChildren == 0
}

func (n *Node4) MaxChildren() int { return 4 }
func (n *Node4) MinChildren() int { return 1 }

func (n *Node4) Grow() Node {
    newNode := &Node16{
        Header: n.Header,
        Keys:   [16]byte{},
    }
    // Copy children
    for i := 0; i < int(n.Header.NumChildren); i++ {
        newNode.Keys[i] = n.Keys[i]
        newNode.Children[i] = n.Children[i]
    }
    newNode.Header.Type = TypeNode16
    return newNode
}

func (n *Node4) Shrink() Node {
    return n
}