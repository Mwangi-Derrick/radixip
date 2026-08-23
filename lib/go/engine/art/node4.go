// node4.go — Node4: 1-4 children (linear key scan, smallest node)
package art

import "unsafe"

const node4Empty = uint8(0xFF)

func NewNode4() *Node4 {
	return &Node4{
		Header: Header{Type: TypeNode4},
	}
}

func (n *Node4) numChildren() int { return int(n.Header.NumChildren) }

func (n *Node4) findChild(b byte) (unsafe.Pointer, bool) {
	for i := 0; i < int(n.Header.NumChildren); i++ {
		if n.Keys[i] == b {
			return n.Children[i], true
		}
	}
	return nil, false
}

func (n *Node4) addChild(b byte, child unsafe.Pointer) Node {
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

func (n *Node4) removeChild(b byte) Node {
	for i := 0; i < int(n.Header.NumChildren); i++ {
		if n.Keys[i] == b {
			last := int(n.Header.NumChildren) - 1
			n.Keys[i] = n.Keys[last]
			n.Children[i] = n.Children[last]
			n.Children[last] = nil
			n.Header.NumChildren--
			return n
		}
	}
	return n
}

func (n *Node4) isFull() bool  { return n.Header.NumChildren >= 4 }
func (n *Node4) isEmpty() bool { return n.Header.NumChildren == 0 }

func (n *Node4) grow() Node {
	newNode := &Node16{
		Header: Header{
			Type:        TypeNode16,
			NumChildren: n.Header.NumChildren,
			PrefixLen:   n.Header.PrefixLen,
			Prefix:      n.Header.Prefix,
		},
	}
	for i := 0; i < int(n.Header.NumChildren); i++ {
		newNode.Keys[i] = n.Keys[i]
		newNode.Children[i] = n.Children[i]
	}
	return newNode
}
