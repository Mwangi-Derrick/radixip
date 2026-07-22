package radixip

import (
	"net"
	"sync/atomic"
	"unsafe"
)

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

type lockFreeNode struct {
	metadata unsafe.Pointer // *Metadata
	left     unsafe.Pointer // *lockFreeNode
	right    unsafe.Pointer // *lockFreeNode
	prefix   unsafe.Pointer // *net.IPNet
	bit      uint8          // Not atomic - read-only after node creation?
	edgeLen  int            // Not atomic - read-only after node creation?
	edgeBits []byte         // Not atomic - read-only after node creation?
}

func NewLockFreeNode() *lockFreeNode {
	return &lockFreeNode{}
}

func (n *lockFreeNode) Bit() *uint8 {
	return &n.bit // Return pointer to the bit field
}

func (n *lockFreeNode) SetBit(bit uint8) {
	// Only call if node is not yet published!
	n.bit = bit
}

func (n *lockFreeNode) Left() RadixNode {
	ptr := atomic.LoadPointer(&n.left)
	if ptr == nil {
		return nil
	}
	return (*lockFreeNode)(ptr)
}

func (n *lockFreeNode) SetLeft(node RadixNode) {
	if node == nil {
		atomic.StorePointer(&n.left, nil)
	} else {
		atomic.StorePointer(&n.left, unsafe.Pointer(node.(*lockFreeNode)))
	}
}

func (n *lockFreeNode) Right() RadixNode {
	ptr := atomic.LoadPointer(&n.right)
	if ptr == nil {
		return nil
	}
	return (*lockFreeNode)(ptr)
}

func (n *lockFreeNode) SetRight(node RadixNode) {
	if node == nil {
		atomic.StorePointer(&n.right, nil)
	} else {
		atomic.StorePointer(&n.right, unsafe.Pointer(node.(*lockFreeNode)))
	}
}

func (n *lockFreeNode) Metadata() *Metadata {
	return (*Metadata)(atomic.LoadPointer(&n.metadata))
}

func (n *lockFreeNode) SetMetadata(metadata *Metadata) {
	atomic.StorePointer(&n.metadata, unsafe.Pointer(metadata))
}

func (n *lockFreeNode) ClearMetadata() {
	atomic.StorePointer(&n.metadata, nil)
}

func (n *lockFreeNode) Prefix() *net.IPNet {
	return (*net.IPNet)(atomic.LoadPointer(&n.prefix))
}

func (n *lockFreeNode) SetPrefix(prefix *net.IPNet) {
	atomic.StorePointer(&n.prefix, unsafe.Pointer(prefix))
}

func (n *lockFreeNode) EdgeLen() int {
	return n.edgeLen
}

func (n *lockFreeNode) EdgeBits() []byte {
	return n.edgeBits
}

func (n *lockFreeNode) SetEdge(bits []byte, length int) {
	n.edgeBits = bits
	n.edgeLen = length
}

func NewAtomicNode() *atomicNode {
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

func (n *atomicNode) EdgeBits() []byte {
	return nil
}

func (n *atomicNode) EdgeLen() int {
	return 0
}

func (n *atomicNode) SetEdge(bits []byte, length int) {}

func NewNormalNode() *normalNode {
	return &normalNode{}
}

func (n *normalNode) Bit() *uint8 {
	return &n.bit
}

func (n *normalNode) SetBit(bit uint8) {
	n.bit = bit
}

func (n *normalNode) Left() RadixNode {
	return n.left
}

func (n *normalNode) SetLeft(node RadixNode) {
	if node == nil {
		n.left = nil
	} else {
		n.left = node.(*normalNode)
	}
}

func (n *normalNode) Right() RadixNode {
	return n.right
}

func (n *normalNode) SetRight(node RadixNode) {
	if node == nil {
		n.right = nil
	} else {
		n.right = node.(*normalNode)
	}
}

func (n *normalNode) Metadata() *Metadata {
	return n.metadata
}

func (n *normalNode) SetMetadata(metadata *Metadata) {
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

func (n *normalNode) EdgeBits() []byte {
	return nil
}

func (n *normalNode) EdgeLen() int {
	return 0
}

func (n *normalNode) SetEdge(bits []byte, length int) {}

func NewPaddedNode() *paddedNode {
	return &paddedNode{}
}

func (n *paddedNode) Bit() *uint8 {
	return &n.bit
}

func (n *paddedNode) SetBit(bit uint8) {
	n.bit = bit
}

func (n *paddedNode) Left() RadixNode {
	return n.left
}

func (n *paddedNode) SetLeft(node RadixNode) {
	if node == nil {
		n.left = nil
	} else {
		n.left = node.(*paddedNode)
	}
}

func (n *paddedNode) Right() RadixNode {
	return n.right
}

func (n *paddedNode) SetRight(node RadixNode) {
	if node == nil {
		n.right = nil
	} else {
		n.right = node.(*paddedNode)
	}
}

func (n *paddedNode) Metadata() *Metadata {
	return n.metadata
}

func (n *paddedNode) SetMetadata(metadata *Metadata) {
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

func (n *paddedNode) EdgeBits() []byte {
	return nil
}

func (n *paddedNode) EdgeLen() int {
	return 0
}

func (n *paddedNode) SetEdge(bits []byte, length int) {}

