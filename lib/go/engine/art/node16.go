// node16.go — Node16: 5-16 children (linear or SIMD key scan)
package art

import "unsafe"

func NewNode16() *Node16 {
	return &Node16{
		Header: Header{Type: TypeNode16},
	}
}

func (n *Node16) numChildren() int { return int(n.Header.NumChildren) }

func (n *Node16) findChild(b byte) (unsafe.Pointer, bool) {
	if useSIMD {
		idx := simdFindKey(&n.Keys, b, n.Header.NumChildren)
		if idx >= 0 {
			return n.Children[idx], true
		}
		return nil, false
	}
	for i := 0; i < int(n.Header.NumChildren); i++ {
		if n.Keys[i] == b {
			return n.Children[i], true
		}
	}
	return nil, false
}

func (n *Node16) addChild(b byte, child unsafe.Pointer) Node {
	if n.isFull() {
		grown := n.grow()
		return grown.addChild(b, child)
	}
	idx := n.Header.NumChildren
	n.Keys[idx] = b
	n.Children[idx] = child
	n.Header.NumChildren++
	return n
}

func (n *Node16) removeChild(b byte) Node {
	for i := 0; i < int(n.Header.NumChildren); i++ {
		if n.Keys[i] == b {
			last := int(n.Header.NumChildren) - 1
			n.Keys[i] = n.Keys[last]
			n.Children[i] = n.Children[last]
			n.Children[last] = nil
			n.Header.NumChildren--
			if n.Header.NumChildren < 4 {
				return n.shrink()
			}
			return n
		}
	}
	return n
}

func (n *Node16) isFull() bool  { return n.Header.NumChildren >= 16 }
func (n *Node16) isEmpty() bool { return n.Header.NumChildren == 0 }

func (n *Node16) grow() Node {
	newNode := NewNode48()
	newNode.Header.PrefixLen = n.Header.PrefixLen
	newNode.Header.Prefix = n.Header.Prefix
	for i := 0; i < int(n.Header.NumChildren); i++ {
		b := n.Keys[i]
		newNode.Index[b] = byte(i)
		newNode.Children[i] = n.Children[i]
	}
	newNode.Header.NumChildren = n.Header.NumChildren
	return newNode
}

func (n *Node16) shrink() Node {
	newNode := NewNode4()
	newNode.Header.PrefixLen = n.Header.PrefixLen
	newNode.Header.Prefix = n.Header.Prefix
	for i := 0; i < int(n.Header.NumChildren); i++ {
		newNode.Keys[i] = n.Keys[i]
		newNode.Children[i] = n.Children[i]
	}
	newNode.Header.NumChildren = n.Header.NumChildren
	return newNode
}