package go

import (
	"net"
	"net/ip"
	"sync/atomic"
	"unsafe"
)

type StandardEngine struct {
	root        RadixNode
	entries     sync.RWMutex
	entriesMap  map[IpNetwork]Metadata
	size        int64
	stats       sync.RWMutex
	statsData   EngineStats
	nodeBuilder *NodeBuilder
}

func NewStandardEngine(nodeVariant NodeVariant) *StandardEngine {
	builder := NewNodeBuilder(nodeVariant)
	return &StandardEngine{
		root:        builder.Build(),
		entriesMap:  make(map[IpNetwork]Metadata),
		size:        0,
		statsData:   EngineStats{},
		nodeBuilder: builder,
	}
}

