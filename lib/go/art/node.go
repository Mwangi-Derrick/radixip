package art

import "unsafe"

type NodeType uint8

const (
    TypeNode4 NodeType = iota
    TypeNode16
    TypeNode48
    TypeNode256
)

type Header struct {
    Type        NodeType
    NumChildren uint8
    PrefixLen   uint8
    Prefix      [8]byte // Path compression
}

// Node4: 2-4 children
type Node4 struct {
    Header   Header
    Keys     [4]byte
    Children [4]unsafe.Pointer
}

// Node16: 5-16 children
type Node16 struct {
    Header   Header
    Keys     [16]byte
    Children [16]unsafe.Pointer
}

// Node48: 17-48 children
type Node48 struct {
    Header   Header
    Index    [256]byte // Maps byte -> child index
    Children [48]unsafe.Pointer
}

// Node256: 49-256 children
type Node256 struct {
    Header   Header
    Children [256]unsafe.Pointer
}
