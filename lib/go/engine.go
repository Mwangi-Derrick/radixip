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

func (e *StandardEngine) insertRecursive(node RadixNode, network *IpNetwork, metadata Metadata, bitPos int) RadixNode {
	child := e.nodeBuilder.BuildLeaf(*network, metadata)
	node.InsertChild(*network, child)
	return child
}

func (e *StandardEngine) lookupRecursive(node RadixNode, ip *net.IP, bitPos int) *Metadata {
	e.entries.RLock()
	defer e.entries.RUnlock()
	return LongestPrefixMatchEntries(e.entriesMap, ip)
}

// RadixEngine implementation
func (e *StandardEngine) Insert(prefix IpNetwork, metadata Metadata) error {
	e.entries.Lock()
	_, isNew := e.entriesMap[prefix]
	e.entriesMap[prefix] = metadata
	e.entries.Unlock()

	child := e.nodeBuilder.BuildLeaf(prefix, metadata)
	e.root.InsertChild(prefix, child)

	if !isNew {
		atomic.AddInt64(&e.size, 1)
	}

	e.stats.Lock()
	e.statsData.Inserts++
	e.statsData.Size = e.Size()
	e.stats.Unlock()

	return nil
}

func (e *StandardEngine) Lookup(ip *net.IP) *Metadata {
	e.entries.RLock()
	result := LongestPrefixMatchEntries(e.entriesMap, ip)
	e.entries.RUnlock()

	e.stats.Lock()
	e.statsData.Lookups++
	if result != nil {
		e.statsData.Hits++
	} else {
		e.statsData.Misses++
	}
	e.stats.Unlock()

	return result
}

func (e *StandardEngine) Remove(prefix *IpNetwork) *Metadata {
	e.entries.Lock()
	removed, exists := e.entriesMap[*prefix]
	delete(e.entriesMap, *prefix)
	e.entries.Unlock()

	e.root.RemoveChild(prefix)

	if exists {
		atomic.AddInt64(&e.size, -1)
		e.stats.Lock()
		e.statsData.Removals++
		e.statsData.Size = e.Size()
		e.stats.Unlock()
	}

	if exists {
		return &removed
	}
	return nil
}

func (e *StandardEngine) Contains(prefix *IpNetwork) bool {
	e.entries.RLock()
	defer e.entries.RUnlock()
	_, exists := e.entriesMap[*prefix]
	return exists
}

func (e *StandardEngine) Clear() {
	e.entries.Lock()
	e.entriesMap = make(map[IpNetwork]Metadata)
	e.entries.Unlock()

	atomic.StoreInt64(&e.size, 0)

	e.stats.Lock()
	e.statsData.Size = 0
	e.stats.Unlock()
}

func (e *StandardEngine) Size() int64 {
	return atomic.LoadInt64(&e.size)
}

func (e *StandardEngine) Stats() EngineStats {
	e.stats.RLock()
	defer e.stats.RUnlock()
	stats := e.statsData
	stats.Size = e.Size()
	return stats
}

