package node

import (
	"net"

	radixIp "github.com/Mwangi-Derrick/radixip/lib/go"
)

//
// UNCOMPRESSED TREE
//
// Each prefix is a full path from root to leaf.
// O(P) where P = max prefix length (128 for IPv6), regardless of branching.
// Best for heavy modification workloads where tree shape changes often.

type UncompressedTree struct {
	root        radixIp.RadixNode
	nodeBuilder *radixIp.NodeBuilder
}

func NewUncompressedTree(nodeVariant radixIp.NodeVariant) *UncompressedTree {
	builder := radixIp.NewNodeBuilder(nodeVariant)
	return &UncompressedTree{
		root:        builder.Build(),
		nodeBuilder: builder,
	}
}

func (t *UncompressedTree) Insert(prefix radixIp.IpNetwork, metadata radixIp.Metadata) (bool, error) {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := t.root

	for depth := 0; depth < prefixLen; depth++ {
		bit := t.getBit(ip, depth)
		var next radixIp.RadixNode
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

func (t *UncompressedTree) Lookup(ip *net.IP) *radixIp.Metadata {
	var bestMatch *radixIp.Metadata
	current := t.root
	depth := 0

	for current != nil {
		if p := current.Prefix(); p != nil {
			if p.Contains(*ip) {
				bestMatch = current.Metadata()
			}
		}

		bit := t.getBit(*ip, depth)
		if bit == 0 {
			current = current.Left()
		} else {
			current = current.Right()
		}
		depth++
	}

	return bestMatch
}

func (t *UncompressedTree) Remove(prefix *radixIp.IpNetwork) *radixIp.Metadata {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := t.root
	for depth := 0; depth < prefixLen; depth++ {
		bit := t.getBit(ip, depth)
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

func (t *UncompressedTree) Contains(prefix *radixIp.IpNetwork) bool {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := t.root
	for depth := 0; depth < prefixLen; depth++ {
		bit := t.getBit(ip, depth)
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

// longestPrefixMatch is now implemented directly in UncompressedTree and CompressedTree Lookups

// t.getBit returns the bit at the specified position from an IP
func (t *UncompressedTree) getBit(ip net.IP, bitPos int) int {
	// Convert IP to byte slice
	ipBytes := ip.To4()
	if ipBytes == nil {
		ipBytes = ip.To16()
	}

	if ipBytes == nil {
		return 0
	}

	// Find the byte and bit within the byte
	byteIdx := bitPos / 8
	// we count bits from left to right( most significant to least significant )
	bitIdx := 7 - (bitPos % 8) // Most significant bit first

	if byteIdx >= len(ipBytes) {
		return 0
	}

	if (ipBytes[byteIdx]>>bitIdx)&1 == 1 {
		return 1
	}
	return 0
}
