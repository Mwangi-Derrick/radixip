package node

import "net"

func getBitFromBytes(b []byte, pos int) uint8 {
	byteIdx := pos / 8
	bitIdx := 7 - (pos % 8)
	if byteIdx >= len(b) {
		return 0
	}
	return (b[byteIdx] >> uint(bitIdx)) & 1
}

func extractBits(b []byte, start, length int) []byte {
	byteCount := (length + 7) / 8
	out := make([]byte, byteCount)
	for i := 0; i < length; i++ {
		bit := getBitFromBytes(b, start+i)
		byteI := i / 8
		bitI := 7 - (i % 8)
		out[byteI] |= bit << uint(bitI)
	}
	return out
}

func commonPrefixLen(a []byte, aLen int, bb []byte, bLen int) int {
	max := aLen
	if bLen < max {
		max = bLen
	}
	for i := 0; i < max; i++ {
		if getBitFromBytes(a, i) != getBitFromBytes(bb, i) {
			return i
		}
	}
	return max
}

func ipToBytes(ip net.IP) []byte {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip.To16()
}
