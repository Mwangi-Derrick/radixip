package node

import (
	"net"

	types "github.com/Mwangi-Derrick/radixip/lib/go"
)

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
