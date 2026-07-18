package radixip

import (
	"net"
	"sync/atomic"
	"unsafe"
)

// IpNetwork represents a CIDR network (similar to ipnetwork::IpNetwork)
type IpNetwork struct {
	IP   net.IP
	Mask net.IPMask
}

// NewIpNetwork creates a new IpNetwork from CIDR string
func NewIpNetwork(cidr string) (IpNetwork, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return IpNetwork{}, err
	}
	return IpNetwork{
		IP:   ip,
		Mask: ipnet.Mask,
	}, nil
}

// String returns the CIDR notation
func (n IpNetwork) String() string {
	return (&net.IPNet{
		IP:   n.IP,
		Mask: n.Mask,
	}).String()
}

// Equal checks if two IpNetworks are equal
func (n IpNetwork) Equal(other IpNetwork) bool {
	return n.IP.Equal(other.IP) &&
		len(n.Mask) == len(other.Mask) &&
		bytesEqual(n.Mask, other.Mask)
}

// IpNetworkKey is a comparable key for maps (since IpNetwork contains slices)
type IpNetworkKey struct {
	IP   string
	Mask string
}

// NodeBuilder is a factory for RadixNodes
type NodeBuilder struct {
	variant NodeVariant
}

func NewNodeBuilder(variant NodeVariant) *NodeBuilder {
	return &NodeBuilder{variant: variant}
}

func (b *NodeBuilder) Build() RadixNode {
	// For now, always return atomicNode, we can add paddedNode or normalNode later if needed
	return newAtomicNode()
}

// atomicNode implements RadixNode using atomic operations for thread safety
type atomicNode struct {
	bit      unsafe.Pointer // *uint8
	left     unsafe.Pointer // *atomicNode
	right    unsafe.Pointer // *atomicNode
	metadata unsafe.Pointer // *Metadata
	prefix   unsafe.Pointer // *net.IPNet
}

type normalNode struct {
	bit      uint8
	left     *normalNode
	right    *normalNode
	metadata *Metadata
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
	metadata *Metadata
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

func (n *atomicNode) Left() RadixNode {
	ptr := atomic.LoadPointer(&n.left)
	if ptr == nil {
		return nil
	}
	return (*atomicNode)(ptr)
}

func (n *atomicNode) SetLeft(node RadixNode) {
	if node == nil {
		atomic.StorePointer(&n.left, nil)
	} else {
		atomic.StorePointer(&n.left, unsafe.Pointer(node.(*atomicNode)))
	}
}

func (n *atomicNode) Right() RadixNode {
	ptr := atomic.LoadPointer(&n.right)
	if ptr == nil {
		return nil
	}
	return (*atomicNode)(ptr)
}

func (n *atomicNode) SetRight(node RadixNode) {
	if node == nil {
		atomic.StorePointer(&n.right, nil)
	} else {
		atomic.StorePointer(&n.right, unsafe.Pointer(node.(*atomicNode)))
	}
}

func (n *atomicNode) Metadata() *Metadata {
	return (*Metadata)(atomic.LoadPointer(&n.metadata))
}

func (n *atomicNode) SetMetadata(metadata *Metadata) {
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

// Helper function to convert IpNetwork to map key
func ipNetworkToKey(network IpNetwork) IpNetworkKey {
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
