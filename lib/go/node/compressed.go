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

type CompressedPaddedNode struct {
	_pad1    [64]byte
	metadata unsafe.Pointer // *radix.Metadata
	_pad2    [56]byte
	left     *CompressedPaddedNode
	_pad3    [56]byte
	right    *CompressedPaddedNode
	_pad4    [56]byte
	prefix   *net.IPNet
	_pad5    [56]byte
	edgeLen  int
	_pad6    [56]byte
	edgeBits []byte
	_pad7    [64]byte
}

func (n *CompressedPaddedNode) Metadata() *radix.Metadata {
	return (*radix.Metadata)(atomic.LoadPointer(&n.metadata))
}

func (n *CompressedPaddedNode) SetMetadata(metadata *radix.Metadata) {
	atomic.StorePointer(&n.metadata, unsafe.Pointer(metadata))
}

func (n *CompressedPaddedNode) ClearMetadata() {
	atomic.StorePointer(&n.metadata, nil)
}

func (n *CompressedPaddedNode) GetLeft() *CompressedPaddedNode {
	return n.left
}

func (n *CompressedPaddedNode) GetRight() *CompressedPaddedNode {
	return n.right
}

func (n *CompressedPaddedNode) SetLeft(left *CompressedPaddedNode) {
	n.left = left
}

func (n *CompressedPaddedNode) SetRight(right *CompressedPaddedNode) {
	n.right = right
}

func (n *CompressedPaddedNode) Prefix() *net.IPNet {
	return n.prefix
}

func (n *CompressedPaddedNode) SetPrefix(prefix *net.IPNet) {
	n.prefix = prefix
}

func (n *CompressedPaddedNode) EdgeLen() int {
	return n.edgeLen
}

func (n *CompressedPaddedNode) SetEdgeLen(edgeLen int) {
	n.edgeLen = edgeLen
}

func (n *CompressedPaddedNode) EdgeBits() []byte {
	return n.edgeBits
}

func (n *CompressedPaddedNode) SetEdgeBits(edgeBits []byte) {
	n.edgeBits = edgeBits
}
