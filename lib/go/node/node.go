package node

import (
	"net"
	"sync/atomic"
	"unsafe"

	types "github.com/Mwangi-Derrick/radixip/lib/go"
)

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

// atomicNode implements RadixNode using atomic operations for thread safety
type atomicNode struct {
	bit      unsafe.Pointer // *uint8
	left     unsafe.Pointer // *atomicNode
	right    unsafe.Pointer // *atomicNode
	metadata unsafe.Pointer // *types.Metadata
	prefix   unsafe.Pointer // *net.IPNet
}

type normalNode struct {
	bit      uint8
	left     *normalNode
	right    *normalNode
	metadata *types.Metadata
	prefix   *net.IPNet
}

type paddedNode struct {
	_pad1    [64]byte
	bit      uint8
	_pad2    [56]byte
	left     *paddedNode
	_pad3    [56]byte
	right    *paddedNode
	_pad4    [56]byte
	metadata *types.Metadata
	_pad5    [56]byte
	prefix   *net.IPNet
	_pad6    [56]byte
}

func newAtomicNode() *atomicNode {
	return &atomicNode{}
}

func (n *atomicNode) Bit() *uint8 {
	return (*uint8)(atomic.LoadPointer(&n.bit))
}

func (n *atomicNode) SetBit(bit uint8) {
	bitVal := bit
	atomic.StorePointer(&n.bit, unsafe.Pointer(&bitVal))
}

func (n *atomicNode) Left() types.RadixNode {
	ptr := atomic.LoadPointer(&n.left)
	if ptr == nil {
		return nil
	}
	return (*atomicNode)(ptr)
}

func (n *atomicNode) SetLeft(node types.RadixNode) {
	if node == nil {
		atomic.StorePointer(&n.left, nil)
	} else {
		atomic.StorePointer(&n.left, unsafe.Pointer(node.(*atomicNode)))
	}
}

func (n *atomicNode) Right() types.RadixNode {
	ptr := atomic.LoadPointer(&n.right)
	if ptr == nil {
		return nil
	}
	return (*atomicNode)(ptr)
}

func (n *atomicNode) SetRight(node types.RadixNode) {
	if node == nil {
		atomic.StorePointer(&n.right, nil)
	} else {
		atomic.StorePointer(&n.right, unsafe.Pointer(node.(*atomicNode)))
	}
}

func (n *atomicNode) Metadata() *types.Metadata {
	return (*types.Metadata)(atomic.LoadPointer(&n.metadata))
}

func (n *atomicNode) SetMetadata(metadata *types.Metadata) {
	atomic.StorePointer(&n.metadata, unsafe.Pointer(metadata))
}

func (n *atomicNode) ClearMetadata() {
	atomic.StorePointer(&n.metadata, nil)
}

func (n *atomicNode) Prefix() *net.IPNet {
	return (*net.IPNet)(atomic.LoadPointer(&n.prefix))
}

func (n *atomicNode) SetPrefix(prefix *net.IPNet) {
	atomic.StorePointer(&n.prefix, unsafe.Pointer(prefix))
}

func newNormalNode() *normalNode {
	return &normalNode{}
}

func (n *normalNode) Bit() *uint8 {
	return &n.bit
}

func (n *normalNode) SetBit(bit uint8) {
	n.bit = bit
}

func (n *normalNode) Left() types.RadixNode {
	return n.left
}

func (n *normalNode) SetLeft(node types.RadixNode) {
	n.left = node.(*normalNode)
}

func (n *normalNode) Right() types.RadixNode {
	return n.right
}

func (n *normalNode) SetRight(node types.RadixNode) {
	n.right = node.(*normalNode)
}

func (n *normalNode) Metadata() *types.Metadata {
	return n.metadata
}

func (n *normalNode) SetMetadata(metadata *types.Metadata) {
	n.metadata = metadata
}

func (n *normalNode) ClearMetadata() {
	n.metadata = nil
}

func (n *normalNode) Prefix() *net.IPNet {
	return n.prefix
}

func (n *normalNode) SetPrefix(prefix *net.IPNet) {
	n.prefix = prefix
}

func newPaddedNode() *paddedNode {
	return &paddedNode{}
}

func (n *paddedNode) Bit() *uint8 {
	return &n.bit
}

func (n *paddedNode) SetBit(bit uint8) {
	n.bit = bit
}

func (n *paddedNode) Left() types.RadixNode {
	return n.left
}

func (n *paddedNode) SetLeft(node types.RadixNode) {
	n.left = node.(*paddedNode)
}

func (n *paddedNode) Right() types.RadixNode {
	return n.right
}

func (n *paddedNode) SetRight(node types.RadixNode) {
	n.right = node.(*paddedNode)
}

func (n *paddedNode) Metadata() *types.Metadata {
	return n.metadata
}

func (n *paddedNode) SetMetadata(metadata *types.Metadata) {
	n.metadata = metadata
}

func (n *paddedNode) ClearMetadata() {
	n.metadata = nil
}

func (n *paddedNode) Prefix() *net.IPNet {
	return n.prefix
}

func (n *paddedNode) SetPrefix(prefix *net.IPNet) {
	n.prefix = prefix
}

// NodeBuilder is a factory for RadixNodes
type NodeBuilder struct {
	variant types.NodeVariant
}

func NewNodeBuilder(variant types.NodeVariant) *NodeBuilder {
	return &NodeBuilder{variant: variant}
}

func (b *NodeBuilder) Build() types.RadixNode {
	// For now, always return atomicNode, we can add paddedNode or normalNode later if needed
	return newAtomicNode()
}

// NewIpNetwork creates a new IpNetwork from CIDR string
func NewIpNetwork(cidr string) (types.IpNetwork, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return types.IpNetwork{}, err
	}
	return types.IpNetwork{
		IP:   ip,
		Mask: ipnet.Mask,
	}, nil
}

// String returns the CIDR notation
func (n types.IpNetwork) String() string {
	return (&net.IPNet{
		IP:   n.IP,
		Mask: n.Mask,
	}).String()
}

// Equal checks if two IpNetworks are equal
func (n types.IpNetwork) Equal(other types.IpNetwork) bool {
	return n.IP.Equal(other.IP) &&
		len(n.Mask) == len(other.Mask) &&
		bytesEqual(n.Mask, other.Mask)
}

// IpNetworkKey is a comparable key for maps (since IpNetwork contains slices)
type IpNetworkKey struct {
	IP   string
	Mask string
}

// Helper function to convert IpNetwork to map key
func ipNetworkToKey(network types.IpNetwork) IpNetworkKey {
	return IpNetworkKey{
		IP:   network.IP.String(),
		Mask: maskToString(network.Mask),
	}
}

// Helper function to convert mask to string
func maskToString(mask net.IPMask) string {
	ones, bits := mask.Size()
	if ones == 0 && bits == 0 {
		return ""
	}
	return net.IP(mask).String()
}

// Helper function to compare byte slices
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
