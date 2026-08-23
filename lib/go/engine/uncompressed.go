package radixip

import (
	"net"
	"sync/atomic"
	"unsafe"
)

// atomicNode implements Node using atomic operations for thread safety
type atomicNode struct {
	bit      unsafe.Pointer // *uint8
	left     unsafe.Pointer // *atomicNode
	right    unsafe.Pointer // *atomicNode
	metadata unsafe.Pointer // *Metadata
	prefix   unsafe.Pointer // *net.IPNet
	edgeLen  int            // Not atomic - read-only after node creation?
	edgeBits []byte         // Not atomic - read-only after node creation?
}

type normalNode struct {
	bit      uint8
	left     *normalNode
	right    *normalNode
	metadata *Metadata
	prefix   *net.IPNet
	edgeLen  int    // Not atomic - read-only after node creation?
	edgeBits []byte // Not atomic - read-only after node creation?
}

type paddedNode struct {
	// Only bit needs atomic access/cache line isolation
	bit  uint8
	_pad [63]byte

	// Everything else packed normally - they're read-only!
	left     *paddedNode
	right    *paddedNode
	metadata *Metadata
	prefix   *net.IPNet
	edgeLen  int
	edgeBits []byte
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
	if n == nil {
		return
	}
	// Only call if node is not yet published!
	n.bit = bit
}

func (n *lockFreeNode) Left() Node {
	ptr := atomic.LoadPointer(&n.left)
	if ptr == nil {
		return nil
	}
	return (*lockFreeNode)(ptr)
}

func (n *lockFreeNode) SetLeft(node Node) {
	if n == nil {
		return
	}
	if node == nil {
		atomic.StorePointer(&n.left, nil)
	} else {
		atomic.StorePointer(&n.left, unsafe.Pointer(node.(*lockFreeNode)))
	}
}

func (n *lockFreeNode) Right() Node {
	ptr := atomic.LoadPointer(&n.right)
	if ptr == nil {
		return nil
	}
	return (*lockFreeNode)(ptr)
}

func (n *lockFreeNode) SetRight(node Node) {
	if n == nil {
		return
	}
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
	if n == nil {
		return
	}
	atomic.StorePointer(&n.metadata, unsafe.Pointer(metadata))
}

func (n *lockFreeNode) ClearMetadata() {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.metadata, nil)
}

func (n *lockFreeNode) Prefix() *net.IPNet {
	return (*net.IPNet)(atomic.LoadPointer(&n.prefix))
}

func (n *lockFreeNode) SetPrefix(prefix *net.IPNet) {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.prefix, unsafe.Pointer(prefix))
}

func (n *lockFreeNode) EdgeLen() int {
	return n.edgeLen
}

func (n *lockFreeNode) EdgeBits() []byte {
	return n.edgeBits
}

func (n *lockFreeNode) SetEdge(bits []byte, length int) {
	if n == nil {
		return
	}
	n.edgeBits = bits
	n.edgeLen = length
}

func NewAtomicNode() *atomicNode {
	return &atomicNode{}
}

func (n *atomicNode) Bit() *uint8 {
	return (*uint8)(atomic.LoadPointer(&n.bit))
}

var bitCacheUncompressed = func() [256]uint8 {
	var cache [256]uint8
	for i := 0; i < 256; i++ {
		cache[i] = uint8(i)
	}
	return cache
}()

func (n *atomicNode) SetBit(bit uint8) {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.bit, unsafe.Pointer(&bitCacheUncompressed[bit]))
}

func (n *atomicNode) Left() Node {
	ptr := atomic.LoadPointer(&n.left)
	if ptr == nil {
		return nil
	}
	return (*atomicNode)(ptr)
}

func (n *atomicNode) SetLeft(node Node) {
	if n == nil {
		return
	}
	if node == nil {
		atomic.StorePointer(&n.left, nil)
	} else {
		atomic.StorePointer(&n.left, unsafe.Pointer(node.(*atomicNode)))
	}
}

func (n *atomicNode) Right() Node {
	ptr := atomic.LoadPointer(&n.right)
	if ptr == nil {
		return nil
	}
	return (*atomicNode)(ptr)
}

func (n *atomicNode) SetRight(node Node) {
	if n == nil {
		return
	}
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
	if n == nil {
		return
	}
	atomic.StorePointer(&n.metadata, unsafe.Pointer(metadata))
}

func (n *atomicNode) ClearMetadata() {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.metadata, nil)
}

func (n *atomicNode) Prefix() *net.IPNet {
	return (*net.IPNet)(atomic.LoadPointer(&n.prefix))
}

func (n *atomicNode) SetPrefix(prefix *net.IPNet) {
	if n == nil {
		return
	}
	atomic.StorePointer(&n.prefix, unsafe.Pointer(prefix))
}

func (n *atomicNode) EdgeBits() []byte {
	return nil
}

func (n *atomicNode) EdgeLen() int {
	return 0
}

func (n *atomicNode) SetEdge(bits []byte, length int) {
	if n == nil {
		return
	}
	// Take the address of the slice header to get a *[]byte, then cast to unsafe.Pointer
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&n.edgeBits)), unsafe.Pointer(&bits))
	atomic.StoreInt32((*int32)(unsafe.Pointer(&n.edgeLen)), int32(length))
}

func NewNormalNode() *normalNode {
	return &normalNode{}
}

func (n *normalNode) Bit() *uint8 {
	return &n.bit
}

func (n *normalNode) SetBit(bit uint8) {
	if n == nil {
		return
	}
	n.bit = bit
}

func (n *normalNode) Left() Node {
	if n == nil {
		return nil
	}
	return n.left
}

func (n *normalNode) SetLeft(node Node) {
	if n == nil {
		return
	}
	if node == nil {
		n.left = nil
	} else {
		n.left = node.(*normalNode)
	}
}

func (n *normalNode) Right() Node {
	if n == nil {
		return nil
	}
	return n.right
}

func (n *normalNode) SetRight(node Node) {
	if n == nil {
		return
	}
	if node == nil {
		n.right = nil
	} else {
		n.right = node.(*normalNode)
	}
}

func (n *normalNode) Metadata() *Metadata {
	if n == nil {
		return nil
	}
	return n.metadata
}

func (n *normalNode) SetMetadata(metadata *Metadata) {
	if n == nil {
		return
	}
	n.metadata = metadata
}

func (n *normalNode) ClearMetadata() {
	if n == nil {
		return
	}
	n.metadata = nil
}

func (n *normalNode) Prefix() *net.IPNet {
	if n == nil {
		return nil
	}
	return n.prefix
}

func (n *normalNode) SetPrefix(prefix *net.IPNet) {
	if n == nil {
		return
	}
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
	if n == nil {
		return
	}
	n.bit = bit
}

func (n *paddedNode) Left() Node {
	if n == nil {
		return nil
	}
	return n.left
}

func (n *paddedNode) SetLeft(node Node) {
	if n == nil {
		return
	}
	if node == nil {
		n.left = nil
	} else {
		n.left = node.(*paddedNode)
	}
}

func (n *paddedNode) Right() Node {
	if n == nil {
		return nil
	}
	return n.right
}

func (n *paddedNode) SetRight(node Node) {
	if n == nil {
		return
	}
	if node == nil {
		n.right = nil
	} else {
		n.right = node.(*paddedNode)
	}
}

func (n *paddedNode) Metadata() *Metadata {
	if n == nil {
		return nil
	}
	return n.metadata
}

func (n *paddedNode) SetMetadata(metadata *Metadata) {
	if n == nil {
		return
	}
	n.metadata = metadata
}

func (n *paddedNode) ClearMetadata() {
	if n == nil {
		return
	}
	n.metadata = nil
}

func (n *paddedNode) Prefix() *net.IPNet {
	if n == nil {
		return nil
	}
	return n.prefix
}

func (n *paddedNode) SetPrefix(prefix *net.IPNet) {
	if n == nil {
		return
	}
	n.prefix = prefix
}

func (n *paddedNode) EdgeBits() []byte {
	return nil
}

func (n *paddedNode) EdgeLen() int {
	return 0
}

func (n *paddedNode) SetEdge(bits []byte, length int) {
	if n == nil {
		return
	}
	n.edgeBits = bits
	n.edgeLen = length
}
