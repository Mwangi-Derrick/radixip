package radixip

import (
	"net"
	"sync"
	"sync/atomic"
	"unsafe"
)

type CompressedNormalNode struct {
	mu       sync.Mutex
	bit      uint8
	edgeBits []byte
	edgeLen  int
	metadata *Metadata
	prefix   *net.IPNet
	left     *CompressedNormalNode
	right    *CompressedNormalNode
}

func NewCompressedNormalNode() *CompressedNormalNode {
	return &CompressedNormalNode{}
}

func (n *CompressedNormalNode) Bit() *uint8 {
	if n == nil {
		return nil
	}
	return &n.bit
}

func (n *CompressedNormalNode) SetBit(bit uint8) {
	n.bit = bit
}

func (n *CompressedNormalNode) Metadata() *Metadata {
	if n == nil {
		return nil
	}
	return n.metadata
}

func (n *CompressedNormalNode) SetMetadata(metadata *Metadata) {
	if n == nil {
		return
	}
	n.metadata = metadata
}

func (n *CompressedNormalNode) ClearMetadata() {
	if n == nil {
		return
	}
	n.metadata = nil
}

func (n *CompressedNormalNode) Left() RadixNode {
	if n == nil {
		return nil
	}
	if n.left == nil {
		return nil
	}
	return n.left
}

func (n *CompressedNormalNode) SetLeft(node RadixNode) {
	if n == nil {
		return
	}
	if node == nil {
		n.left = nil
	} else {
		n.left = node.(*CompressedNormalNode)
	}
}

func (n *CompressedNormalNode) Right() RadixNode {
	if n == nil {
		return nil
	}
	if n.right == nil {
		return nil
	}
	return n.right
}

func (n *CompressedNormalNode) SetRight(node RadixNode) {
	if n == nil {
		return
	}
	if node == nil {
		n.right = nil
	} else {
		n.right = node.(*CompressedNormalNode)
	}
}

func (n *CompressedNormalNode) Prefix() *net.IPNet {
	if n == nil {
		return nil
	}
	return n.prefix
}

func (n *CompressedNormalNode) SetPrefix(prefix *net.IPNet) {
	if n == nil {
		return
	}
	n.prefix = prefix
}

func (n *CompressedNormalNode) EdgeLen() int {
	return n.edgeLen
}

func (n *CompressedNormalNode) EdgeBits() []byte {
	if n == nil {
		return nil
	}
	return n.edgeBits
}

func (n *CompressedNormalNode) SetEdge(bits []byte, length int) {
	if n == nil {
		return
	}
	n.edgeBits = bits
	n.edgeLen = length
}

type CompressedAtomicNode struct {
	bit      unsafe.Pointer // *uint8
	metadata unsafe.Pointer // *Metadata
	left     unsafe.Pointer // *CompressedAtomicNode
	right    unsafe.Pointer // *CompressedAtomicNode
	prefix   unsafe.Pointer // *net.IPNet
	edgeLen  int
	edgeBits []byte
}

func NewCompressedAtomicNode() *CompressedAtomicNode {
	return &CompressedAtomicNode{}
}

func (n *CompressedAtomicNode) Bit() *uint8 {
	if n == nil {
		return nil
	}
	return (*uint8)(atomic.LoadPointer(&n.bit))
}

func (n *CompressedAtomicNode) SetBit(bit uint8) {
	if n == nil {
		return
	}
	bitVal := bit
	atomic.StorePointer(&n.bit, unsafe.Pointer(&bitVal))
}

func (n *CompressedAtomicNode) Metadata() *Metadata {
	if n == nil {
		return nil
	}
	return (*Metadata)(atomic.LoadPointer(&n.metadata))
}

func (n *CompressedAtomicNode) SetMetadata(metadata *Metadata) {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.metadata, unsafe.Pointer(metadata))
}

func (n *CompressedAtomicNode) ClearMetadata() {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.metadata, nil)
}

func (n *CompressedAtomicNode) Left() RadixNode {
	if n == nil {
		return nil
	}
	ptr := atomic.LoadPointer(&n.left)
	if ptr == nil {
		return nil
	}
	return (*CompressedAtomicNode)(ptr)
}

func (n *CompressedAtomicNode) SetLeft(node RadixNode) {
	if n == nil {
		return
	}
	if node == nil {
		atomic.StorePointer(&n.left, nil)
	} else {
		atomic.StorePointer(&n.left, unsafe.Pointer(node.(*CompressedAtomicNode)))
	}
}

func (n *CompressedAtomicNode) Right() RadixNode {
	if n == nil {
		return nil
	}
	ptr := atomic.LoadPointer(&n.right)
	if ptr == nil {
		return nil
	}
	return (*CompressedAtomicNode)(ptr)
}

func (n *CompressedAtomicNode) SetRight(node RadixNode) {
	if n == nil {
		return
	}
	if node == nil {
		atomic.StorePointer(&n.right, nil)
	} else {
		atomic.StorePointer(&n.right, unsafe.Pointer(node.(*CompressedAtomicNode)))
	}
}

func (n *CompressedAtomicNode) Prefix() *net.IPNet {
	if n == nil {
		return nil
	}
	return (*net.IPNet)(atomic.LoadPointer(&n.prefix))
}

func (n *CompressedAtomicNode) SetPrefix(prefix *net.IPNet) {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.prefix, unsafe.Pointer(prefix))
}

func (n *CompressedAtomicNode) EdgeLen() int {

	return n.edgeLen
}

func (n *CompressedAtomicNode) EdgeBits() []byte {
	if n == nil {
		return nil
	}
	return n.edgeBits
}

func (n *CompressedAtomicNode) SetEdge(bits []byte, length int) {
	if n == nil {
		return
	}
	n.edgeBits = bits
	n.edgeLen = length
}

type CompressedPaddedNode struct {
	// Only bit needs special treatment
	bit  uint8
	_pad [63]byte

	// Pack everything else tightly
	metadata unsafe.Pointer
	left     *CompressedPaddedNode
	right    *CompressedPaddedNode
	prefix   *net.IPNet
	edgeLen  int
	edgeBits []byte
}

func NewCompressedPaddedNode() *CompressedPaddedNode {
	return &CompressedPaddedNode{}
}

func (n *CompressedPaddedNode) Bit() *uint8 {
	if n == nil {
		return nil
	}
	return &n.bit
}

func (n *CompressedPaddedNode) SetBit(bit uint8) {
	if n == nil {
		return
	}
	n.bit = bit
}

func (n *CompressedPaddedNode) Metadata() *Metadata {
	if n == nil {
		return nil
	}
	return (*Metadata)(atomic.LoadPointer(&n.metadata))
}

func (n *CompressedPaddedNode) SetMetadata(metadata *Metadata) {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.metadata, unsafe.Pointer(metadata))
}

func (n *CompressedPaddedNode) ClearMetadata() {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.metadata, nil)
}

func (n *CompressedPaddedNode) Left() RadixNode {
	if n == nil {
		return nil
	}
	if n.left == nil {
		return nil
	}
	return n.left
}

func (n *CompressedPaddedNode) SetLeft(node RadixNode) {
	if n == nil {
		return
	}
	if node == nil {
		n.left = nil
	} else {
		n.left = node.(*CompressedPaddedNode)
	}
}

func (n *CompressedPaddedNode) Right() RadixNode {
	if n == nil {
		return nil
	}
	if n.right == nil {
		return nil
	}
	return n.right
}

func (n *CompressedPaddedNode) SetRight(node RadixNode) {
	if n == nil {
		return
	}
	if node == nil {
		n.right = nil
	} else {
		n.right = node.(*CompressedPaddedNode)
	}
}

func (n *CompressedPaddedNode) Prefix() *net.IPNet {
	if n == nil {
		return nil
	}
	return n.prefix
}

func (n *CompressedPaddedNode) SetPrefix(prefix *net.IPNet) {
	if n == nil {
		return
	}
	n.prefix = prefix
}

func (n *CompressedPaddedNode) EdgeLen() int {
	return n.edgeLen
}

func (n *CompressedPaddedNode) EdgeBits() []byte {
	if n == nil {
		return nil
	}
	return n.edgeBits
}

func (n *CompressedPaddedNode) SetEdge(bits []byte, length int) {
	if n == nil {
		return
	}
	n.edgeBits = bits
	n.edgeLen = length
}

type CompressedLockFreeNode struct {
	bit      uint8
	metadata unsafe.Pointer // *Metadata
	left     unsafe.Pointer // *CompressedLockFreeNode
	right    unsafe.Pointer // *CompressedLockFreeNode
	prefix   unsafe.Pointer // *net.IPNet
	edgeLen  int
	edgeBits []byte
}

func NewCompressedLockFreeNode() *CompressedLockFreeNode {
	return &CompressedLockFreeNode{}
}

func (n *CompressedLockFreeNode) Bit() *uint8 {
	if n == nil {
		return nil
	}
	return &n.bit
}

func (n *CompressedLockFreeNode) SetBit(bit uint8) {
	if n == nil {
		return
	}
	n.bit = bit
}

func (n *CompressedLockFreeNode) Metadata() *Metadata {
	if n == nil {
		return nil
	}
	return (*Metadata)(atomic.LoadPointer(&n.metadata))
}

func (n *CompressedLockFreeNode) SetMetadata(metadata *Metadata) {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.metadata, unsafe.Pointer(metadata))
}

func (n *CompressedLockFreeNode) ClearMetadata() {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.metadata, nil)
}

func (n *CompressedLockFreeNode) Left() RadixNode {
	if n == nil {
		return nil
	}
	ptr := atomic.LoadPointer(&n.left)
	if ptr == nil {
		return nil
	}
	return (*CompressedLockFreeNode)(ptr)
}

func (n *CompressedLockFreeNode) SetLeft(node RadixNode) {
	if n == nil {
		return
	}
	if node == nil {
		atomic.StorePointer(&n.left, nil)
	} else {
		atomic.StorePointer(&n.left, unsafe.Pointer(node.(*CompressedLockFreeNode)))
	}
}

func (n *CompressedLockFreeNode) Right() RadixNode {
	if n == nil {
		return nil
	}
	ptr := atomic.LoadPointer(&n.right)
	if ptr == nil {
		return nil
	}
	return (*CompressedLockFreeNode)(ptr)
}

func (n *CompressedLockFreeNode) SetRight(node RadixNode) {
	if n == nil {
		return
	}
	if node == nil {
		atomic.StorePointer(&n.right, nil)
	} else {
		atomic.StorePointer(&n.right, unsafe.Pointer(node.(*CompressedLockFreeNode)))
	}
}

func (n *CompressedLockFreeNode) Prefix() *net.IPNet {
	if n == nil {
		return nil
	}
	return (*net.IPNet)(atomic.LoadPointer(&n.prefix))
}

func (n *CompressedLockFreeNode) SetPrefix(prefix *net.IPNet) {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.prefix, unsafe.Pointer(prefix))
}

func (n *CompressedLockFreeNode) EdgeLen() int {
	return n.edgeLen
}

func (n *CompressedLockFreeNode) EdgeBits() []byte {
	// return edge bits
	if n == nil {
		return nil
	}
	return n.edgeBits
}

func (n *CompressedLockFreeNode) SetEdge(bits []byte, length int) {
	// set edge bits
	// and also set length of the edge bits
	if n == nil {
		return
	}
	n.edgeBits = bits
	n.edgeLen = length
}
