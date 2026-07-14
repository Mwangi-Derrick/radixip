package go

import (
	"net"
	"net/ip"
	"sync/atomic"
	"unsafe"
	"runtime"
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

// SHARDED ENGINE

type ShardedEngine struct {
	shards     []*StandardEngine
	numShards  int
}

func NewShardedEngine(numShards int, nodeVariant NodeVariant) *ShardedEngine {
	shards := make([]*StandardEngine, numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = NewStandardEngine(nodeVariant)
	}
	return &ShardedEngine{
		shards:    shards,
		numShards: numShards,
	}
}


func (e *ShardedEngine) getShard(ip *net.IP) int {
	var hash uint64
	switch {
	case ip.To4() != nil:
		ip4 := ip.To4()
		/*
		Thinking of it as reclaiming their "favourite sitting spots" inside a larger container perfectly captures exactly what the hardware is doing.
		To solidify your intuition, 
		let's look closely at your shifting counts, 
		because you have the concept 100% correct, 
		you just had a tiny typo on the exact numbers for the 3rd and 4th octets:
		1st Octet: Starts at the bottom of its own small container. 
		It shifts 24 slots left to sit at the very top of the 32-bit container.
		2nd Octet: Starts at the bottom. It shifts 16 slots left to sit right next to the 1st octet.
		3rd Octet: Starts at the bottom. 
		It shifts 8 slots left (not 16) to slide into the third position.4th Octet: It shifts 0 slots (it doesn't shift at all, or shifts 8 less than the 3rd) because its "favourite spot" is already at the very bottom!
		
		*/
		hash = uint64(uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3]))
	default:
		ip6 := ip.To16()
		var h uint64
		for i := 0; i < 8; i++ {
			// we use a plynomail rolling hash
			h = h*31 + uint64(ip6[i])
		}
		hash = h
	}
	return int(hash % uint64(e.numShards))
}

func (e *ShardedEngine) Insert(prefix IpNetwork, metadata Metadata) error {
	for _, shard := range e.shards {
		if err := shard.Insert(prefix, metadata); err != nil {
			return err
		}
	}
	return nil
}

func (e *ShardedEngine) Lookup(ip *net.IP) *Metadata {
	shardIdx := e.getShard(ip)
	return e.shards[shardIdx].Lookup(ip)
}

func (e *ShardedEngine) Remove(prefix *IpNetwork) *Metadata {
	var removed *Metadata
	for _, shard := range e.shards {
		shardRemoved := shard.Remove(prefix)
		if removed == nil {
			removed = shardRemoved
		}
	}
	return removed
}

func (e *ShardedEngine) Contains(prefix *IpNetwork) bool {
	if len(e.shards) > 0 {
		return e.shards[0].Contains(prefix)
	}
	return false
}

func (e *ShardedEngine) Clear() {
	for _, shard := range e.shards {
		shard.Clear()
	}
}

func (e *ShardedEngine) Size() int64 {
	if len(e.shards) > 0 {
		return e.shards[0].Size()
	}
	return 0
}

func (e *ShardedEngine) Stats() EngineStats {
	var total EngineStats
	for _, shard := range e.shards {
		stats := shard.Stats()
		total.Lookups += stats.Lookups
		total.Hits += stats.Hits
		total.Misses += stats.Misses
	}
	if len(e.shards) > 0 {
		stats := e.shards[0].Stats()
		total.Inserts = stats.Inserts
		total.Removals = stats.Removals
	}
	total.Size = e.Size()
	return total
}


type EngineWrapper struct {
	standard   *StandardEngine
	concurrent *ShardedEngine
	variant    EngineVariant
}

func NewEngineWrapper(variant EngineVariant, nodeVariant NodeVariant) *EngineWrapper {
	switch variant {
	case EngineVariantStandard:
		return &EngineWrapper{
			standard: NewStandardEngine(nodeVariant),
			variant:  EngineVariantStandard,
		}
	case EngineVariantConcurrent:
		return &EngineWrapper{
			concurrent: NewShardedEngine(16, nodeVariant),
			variant:    EngineVariantConcurrent,
		}
	case EngineVariantLockFree:
		return &EngineWrapper{
			standard: NewStandardEngine(NodeVariantLockFree),
			variant:  EngineVariantLockFree,
		}
	case EngineVariantAdaptive:
		// Choose based on system characteristics
		cpus := getNumCPU()
		if cpus > 4 {
			return &EngineWrapper{
				concurrent: NewShardedEngine(cpus*2, NodeVariantAtomic),
				variant:    EngineVariantConcurrent,
			}
		} else {
			return &EngineWrapper{
				standard: NewStandardEngine(NodeVariantAtomic),
				variant:  EngineVariantStandard,
			}
		}
	default:
		return &EngineWrapper{
			standard: NewStandardEngine(nodeVariant),
			variant:  EngineVariantStandard,
		}
	}
}

// Helper functions and types
func getNumCPU() int {
	// Get the number of available logical CPUs
	cpus := runtime.NumCPU()
	fmt.Printf("Number of logical CPUs: %d\n", cpus)
}