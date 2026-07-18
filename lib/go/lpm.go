package radixip

import "net"

// longestPrefixMatch is now implemented directly in UncompressedTree and CompressedTree Lookups

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
