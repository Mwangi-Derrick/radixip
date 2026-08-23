// node256.go — Node256: 49-256 children (direct-indexed array, O(1) all ops)
package art

import "unsafe"

func NewNode256() *Node256 {
	return &Node256{
		Header: Header{Type: TypeNode256},
	}
}

func (n *Node256) numChildren() int { return int(n.Header.NumChildren) }

func (n *Node256) findChild(b byte) (unsafe.Pointer, bool) {
	child := n.Children[b]
	if child != nil {
		return child, true
	}
	return nil, false
}

func (n *Node256) addChild(b byte, child unsafe.Pointer) Node {
	if n.Children[b] == nil {
		n.Header.NumChildren++
	}
	n.Children[b] = child
	return n
}

func (n *Node256) removeChild(b byte) Node {
	if n.Children[b] != nil {
		n.Children[b] = nil
		n.Header.NumChildren--

		if n.Header.NumChildren < 49 {
			return n.shrink()
		}
	}
	return n
}

func (n *Node256) isFull() bool  { return false } // Node256 never needs to grow
func (n *Node256) isEmpty() bool { return n.Header.NumChildren == 0 }

// grow is a no-op; Node256 is the largest node type.
func (n *Node256) grow() Node { return n }

func (n *Node256) shrink() Node {
	newNode := NewNode48()
	newNode.Header.PrefixLen = n.Header.PrefixLen
	newNode.Header.Prefix = n.Header.Prefix
	var slot uint8
	for i := 0; i < 256; i++ {
		if n.Children[i] != nil {
			newNode.Index[i] = slot
			newNode.Children[slot] = n.Children[i]
			slot++
		}
	}
	newNode.Header.NumChildren = slot
	return newNode
}