package go

import (
	"net"
)

type UncompressedTree struct {
	root        RadixNode
	nodeBuilder *NodeBuilder
}

func NewUncompressedTree(nodeVariant NodeVariant) *UncompressedTree {
	builder := NewNodeBuilder(nodeVariant)
	return &UncompressedTree{
		root:        builder.Build(),
		nodeBuilder: builder,
	}
}

func (t *UncompressedTree) Insert(prefix IpNetwork, metadata Metadata) (bool, error) {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := t.root

	for depth := 0; depth < prefixLen; depth++ {
		bit := getBit(ip, depth)
		var next RadixNode
		if bit == 0 {
			next = current.Left()
		} else {
			next = current.Right()
		}

		if next != nil {
			current = next
		} else {
			newNode := t.nodeBuilder.Build()
			if bit == 0 {
				current.SetLeft(newNode)
			} else {
				current.SetRight(newNode)
			}
			current = newNode
		}
	}

	isNew := current.Metadata() == nil
	
	netPrefix := net.IPNet{IP: prefix.IP, Mask: prefix.Mask}
	current.SetPrefix(&netPrefix)
	current.SetMetadata(&metadata)

	return isNew, nil
}

func (t *UncompressedTree) Lookup(ip *net.IP) *Metadata {
	var bestMatch *Metadata
	current := t.root
	depth := 0

	for current != nil {
		if p := current.Prefix(); p != nil {
			if p.Contains(*ip) {
				bestMatch = current.Metadata()
			}
		}

		bit := getBit(*ip, depth)
		if bit == 0 {
			current = current.Left()
		} else {
			current = current.Right()
		}
		depth++
	}

	return bestMatch
}

func (t *UncompressedTree) Remove(prefix *IpNetwork) *Metadata {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := t.root
	for depth := 0; depth < prefixLen; depth++ {
		bit := getBit(ip, depth)
		if bit == 0 {
			current = current.Left()
		} else {
			current = current.Right()
		}
		if current == nil {
			return nil
		}
	}

	removed := current.Metadata()
	if removed != nil {
		current.ClearMetadata()
	}

	return removed
}

func (t *UncompressedTree) Contains(prefix *IpNetwork) bool {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := t.root
	for depth := 0; depth < prefixLen; depth++ {
		bit := getBit(ip, depth)
		if bit == 0 {
			current = current.Left()
		} else {
			current = current.Right()
		}
		if current == nil {
			return false
		}
	}
	return current.Metadata() != nil
}

func (t *UncompressedTree) Clear() {
	t.root.SetLeft(nil)
	t.root.SetRight(nil)
}
