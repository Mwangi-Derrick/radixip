// node48.go — Node48: 17-48 children (256-byte index array for O(1) key lookup)
package art

import "unsafe"

const node48Empty = uint8(0xFF) // sentinel meaning "no child at this key"

func NewNode48() *Node48 {
	n := &Node48{
		Header: Header{Type: TypeNode48},
	}
	// Fill index with sentinel so we can distinguish empty slots
	for i := range n.Index {
		n.Index[i] = node48Empty
	}
	return n
}

func (n *Node48) numChildren() int { return int(n.Header.NumChildren) }

func (n *Node48) findChild(b byte) (unsafe.Pointer, bool) {
	idx := n.Index[b]
	if idx == node48Empty {
		return nil, false
	}
	child := n.Children[idx]
	if child == nil {
		return nil, false
	}
	return child, true
}

func (n *Node48) addChild(b byte, child unsafe.Pointer) Node {
	if n.isFull() {
		grown := n.grow()
		return grown.addChild(b, child)
	}
	// Find a free slot in Children
	slot := uint8(n.Header.NumChildren)
	n.Index[b] = slot
	n.Children[slot] = child
	n.Header.NumChildren++
	return n
}

func (n *Node48) removeChild(b byte) Node {
	idx := n.Index[b]
	if idx == node48Empty {
		return n
	}
	n.Children[idx] = nil
	n.Index[b] = node48Empty
	n.Header.NumChildren--

	// Compact children array to keep slots contiguous for addChild
	// (swap last used slot into the freed slot)
	lastUsed := uint8(n.Header.NumChildren)
	if idx != lastUsed {
		// Find the key that points to the last slot
		for i := range n.Index {
			if n.Index[i] == lastUsed {
				n.Index[i] = idx
				n.Children[idx] = n.Children[lastUsed]
				n.Children[lastUsed] = nil
				break
			}
		}
	}

	if n.Header.NumChildren < 17 {
		return n.shrink()
	}
	return n
}

func (n *Node48) isFull() bool  { return n.Header.NumChildren >= 48 }
func (n *Node48) isEmpty() bool { return n.Header.NumChildren == 0 }

func (n *Node48) grow() Node {
	newNode := NewNode256()
	newNode.Header.PrefixLen = n.Header.PrefixLen
	newNode.Header.Prefix = n.Header.Prefix
	for i := 0; i < 256; i++ {
		idx := n.Index[i]
		if idx != node48Empty && n.Children[idx] != nil {
			newNode.Children[i] = n.Children[idx]
			newNode.Header.NumChildren++
		}
	}
	return newNode
}

func (n *Node48) shrink() Node {
	newNode := NewNode16()
	newNode.Header.PrefixLen = n.Header.PrefixLen
	newNode.Header.Prefix = n.Header.Prefix
	var slot int
	for i := 0; i < 256; i++ {
		idx := n.Index[i]
		if idx != node48Empty && n.Children[idx] != nil {
			newNode.Keys[slot] = byte(i)
			newNode.Children[slot] = n.Children[idx]
			slot++
		}
	}
	newNode.Header.NumChildren = uint8(slot)
	return newNode
}