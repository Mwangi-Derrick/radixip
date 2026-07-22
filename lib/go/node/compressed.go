package node

import (
	"net"
	"sync"
	"sync/atomic"
	"unsafe"

	radix "github.com/Mwangi-Derrick/radixip/lib/go"
)

type CompressedNormalNode struct {
	mu       sync.Mutex
	edgeBits []byte
	edgeLen  int
	metadata *radix.Metadata
	prefix   *net.IPNet
	left     *CompressedNormalNode
	right    *CompressedNormalNode
}

func (n *CompressedNormalNode) Metadata() *radix.Metadata {
	return n.metadata
}

func (n *CompressedNormalNode) SetMetadata(metadata *radix.Metadata) {
	n.metadata = metadata
}

func (n *CompressedNormalNode) ClearMetadata() {
	n.metadata = nil
}

func (n *CompressedNormalNode) GetLeft() *CompressedNormalNode {
	return n.left
}

func (n *CompressedNormalNode) GetRight() *CompressedNormalNode {
	return n.right
}

func (n *CompressedNormalNode) SetLeft(left *CompressedNormalNode) {
	n.left = left
}

func (n *CompressedNormalNode) SetRight(right *CompressedNormalNode) {
	n.right = right
}

func (n *CompressedNormalNode) Prefix() *net.IPNet {
	return n.prefix
}

func (n *CompressedNormalNode) SetPrefix(prefix *net.IPNet) {
	n.prefix = prefix
}

func (n *CompressedNormalNode) EdgeLen() int {
	return n.edgeLen
}

func (n *CompressedNormalNode) SetEdgeLen(edgeLen int) {
	n.edgeLen = edgeLen
}

func (n *CompressedNormalNode) EdgeBits() []byte {
	return n.edgeBits
}

func (n *CompressedNormalNode) SetEdgeBits(edgeBits []byte) {
	n.edgeBits = edgeBits
}

type CompressedAtomicNode struct {
	metadata unsafe.Pointer // *radix.Metadata
	left     *CompressedAtomicNode
	right    *CompressedAtomicNode
	prefix   *net.IPNet
	edgeLen  int
	edgeBits []byte
}

func (n *CompressedAtomicNode) Metadata() *radix.Metadata {
	return (*radix.Metadata)(atomic.LoadPointer(&n.metadata))
}

func (n *CompressedAtomicNode) SetMetadata(metadata *radix.Metadata) {
	atomic.StorePointer(&n.metadata, unsafe.Pointer(metadata))
}

func (n *CompressedAtomicNode) ClearMetadata() {
	atomic.StorePointer(&n.metadata, nil)
}

func (n *CompressedAtomicNode) GetLeft() *CompressedAtomicNode {
	return n.left
}

func (n *CompressedAtomicNode) GetRight() *CompressedAtomicNode {
	return n.right
}

func (n *CompressedAtomicNode) SetLeft(left *CompressedAtomicNode) {
	n.left = left
}

func (n *CompressedAtomicNode) SetRight(right *CompressedAtomicNode) {
	n.right = right
}

func (n *CompressedAtomicNode) Prefix() *net.IPNet {
	return n.prefix
}

func (n *CompressedAtomicNode) SetPrefix(prefix *net.IPNet) {
	n.prefix = prefix
}

func (n *CompressedAtomicNode) EdgeLen() int {
	return n.edgeLen
}

func (n *CompressedAtomicNode) SetEdgeLen(edgeLen int) {
	n.edgeLen = edgeLen
}

func (n *CompressedAtomicNode) EdgeBits() []byte {
	return n.edgeBits
}

func (n *CompressedAtomicNode) SetEdgeBits(edgeBits []byte) {
	n.edgeBits = edgeBits
}
