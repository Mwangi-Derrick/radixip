package radixip

import "net"

// longestPrefixMatch finds the longest prefix match for an IP
func (n *radixNode) longestPrefixMatch(ip net.IP) (Metadata, bool) {
	if n == nil {
		return nil, false
	}

	// Check if this node matches
	var bestMatch Metadata
	var found bool

	if n.network != nil && n.network.Contains(ip) {
		bestMatch = n.metadata
		found = true
	}

	// If we have a bit position, traverse down
	if n.bit >= 0 {
		bit := getBit(ip, n.bit)
		var child *radixNode

		if bit == 0 && n.left != nil {
			child = (*radixNode)(n.left)
		} else if bit == 1 && n.right != nil {
			child = (*radixNode)(n.right)
		}

		if child != nil {
			if meta, ok := child.longestPrefixMatch(ip); ok {
				// Child found a more specific match
				return meta, true
			}
		}
	}

	// Return the best match found at this level or below
	return bestMatch, found
}

// getBit returns the bit at the specified position from an IP
func getBit(ip net.IP, bitPos int) int {
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
