package go

import (
    "net"
    "sync/atomic"
    "unsafe"
)

// IpNetwork represents a CIDR network (similar to ipnetwork::IpNetwork)
type IpNetwork struct {
    IP     net.IP
    Mask   net.IPMask
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
    return net.IPNet{
        IP:   n.IP,
        Mask: n.Mask,
    }.String()
}

// Equal checks if two IpNetworks are equal
func (n IpNetwork) Equal(other IpNetwork) bool {
    return n.IP.Equal(other.IP) && 
           len(n.Mask) == len(other.Mask) && 
           bytesEqual(n.Mask, other.Mask)
}

// Metadata represents arbitrary metadata (similar to crate::types::Metadata)
type Metadata struct {
    // Add your metadata fields here
    // For example:
    Data interface{}
}

// RadixNode represents a binary radix tree node with 64-byte cache alignment
// Note: Go doesn't have direct control over alignment like Rust's repr(C, align(64))
// but we can use struct padding to achieve similar alignment
type RadixNode struct {
	bit      int          // When to branch
    // Using atomic pointers for thread-safety (similar to Arc in Rust)
    left   unsafe.Pointer // *RadixNode with atomic operations
    right  unsafe.Pointer // *RadixNode with atomic operations
    
    // Metadata (could be nil)
    metadata unsafe.Pointer // *Metadata
    
    // Prefix network (could be nil)
    prefix unsafe.Pointer // *IpNetwork
    
    // Children map - in Go we use a map with IpNetwork as key
    // Note: This is different from Rust's HashMap but serves the same purpose
    children map[IpNetworkKey]*RadixNode
}

// IpNetworkKey is a comparable key for maps (since IpNetwork contains slices)
type IpNetworkKey struct {
    IP   string
    Mask string
}

// NewRadixNode creates a new RadixNode
func NewRadixNode() *RadixNode {
    return &RadixNode{
        left:     nil,
        right:    nil,
        metadata: nil,
        prefix:   nil,
        children: make(map[IpNetworkKey]*RadixNode),
    }
}

// NewRadixNodeWithChildren creates a node with pre-allocated children map
func NewRadixNodeWithChildren(capacity int) *RadixNode {
    return &RadixNode{
        left:     nil,
        right:    nil,
        metadata: nil,
        prefix:   nil,
        children: make(map[IpNetworkKey]*RadixNode, capacity),
    }
}

// GetLeft safely gets the left child
func (n *RadixNode) GetLeft() *RadixNode {
    return (*RadixNode)(atomic.LoadPointer(&n.left))
}

// SetLeft safely sets the left child
func (n *RadixNode) SetLeft(child *RadixNode) {
    atomic.StorePointer(&n.left, unsafe.Pointer(child))
}

// GetRight safely gets the right child
func (n *RadixNode) GetRight() *RadixNode {
    return (*RadixNode)(atomic.LoadPointer(&n.right))
}

// SetRight safely sets the right child
func (n *RadixNode) SetRight(child *RadixNode) {
    atomic.StorePointer(&n.right, unsafe.Pointer(child))
}

// GetMetadata safely gets the metadata
func (n *RadixNode) GetMetadata() *Metadata {
    return (*Metadata)(atomic.LoadPointer(&n.metadata))
}

// SetMetadata safely sets the metadata
func (n *RadixNode) SetMetadata(meta *Metadata) {
    atomic.StorePointer(&n.metadata, unsafe.Pointer(meta))
}

// GetPrefix safely gets the prefix
func (n *RadixNode) GetPrefix() *IpNetwork {
    return (*IpNetwork)(atomic.LoadPointer(&n.prefix))
}

// SetPrefix safely sets the prefix
func (n *RadixNode) SetPrefix(prefix *IpNetwork) {
    atomic.StorePointer(&n.prefix, unsafe.Pointer(prefix))
}

// Clone creates a deep copy of the node (similar to Rust's Clone trait)
func (n *RadixNode) Clone() *RadixNode {
    newNode := &RadixNode{
        children: make(map[IpNetworkKey]*RadixNode, len(n.children)),
    }
    
    // Copy children map
    for key, child := range n.children {
        // Deep copy child nodes if needed, or just share references
        // This is the copy-on-write pattern - we can share children
        // that aren't being modified
        newNode.children[key] = child
    }
    
    // Copy atomic fields
    left := n.GetLeft()
    if left != nil {
        newNode.SetLeft(left)
    }
    
    right := n.GetRight()
    if right != nil {
        newNode.SetRight(right)
    }
    
    meta := n.GetMetadata()
    if meta != nil {
        // Deep copy metadata if needed
        metaCopy := &Metadata{
            Data: meta.Data,
        }
        newNode.SetMetadata(metaCopy)
    }
    
    prefix := n.GetPrefix()
    if prefix != nil {
        // Deep copy prefix
        prefixCopy := &IpNetwork{
            IP:   make(net.IP, len(prefix.IP)),
            Mask: make(net.IPMask, len(prefix.Mask)),
        }
        copy(prefixCopy.IP, prefix.IP)
        copy(prefixCopy.Mask, prefix.Mask)
        newNode.SetPrefix(prefixCopy)
    }
    
    return newNode
}

// CloneWithInsert creates a clone with a new subnet inserted (copy-on-write pattern)
func (n *RadixNode) CloneWithInsert(network IpNetwork, metadata Metadata) *RadixNode {
    // Create a clone of the current node
    newNode := n.Clone()
    
    // Insert the new subnet into the cloned node
    // This is where you'd implement your insertion logic
    key := ipNetworkToKey(network)
    newNode.children[key] = &RadixNode{
        metadata: unsafe.Pointer(&metadata),
        prefix:   unsafe.Pointer(&network),
        children: make(map[IpNetworkKey]*RadixNode),
    }
    
    return newNode
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

// GetChild retrieves a child by network
func (n *RadixNode) GetChild(network IpNetwork) *RadixNode {
    key := ipNetworkToKey(network)
    return n.children[key]
}

// AddChild adds a child node
func (n *RadixNode) AddChild(network IpNetwork, child *RadixNode) {
    key := ipNetworkToKey(network)
    n.children[key] = child
}

// RemoveChild removes a child node
func (n *RadixNode) RemoveChild(network IpNetwork) {
    key := ipNetworkToKey(network)
    delete(n.children, key)
}

// HasChildren checks if the node has any children
func (n *RadixNode) HasChildren() bool {
    return len(n.children) > 0
}

// GetChildren returns all children
func (n *RadixNode) GetChildren() map[IpNetworkKey]*RadixNode {
    return n.children
}