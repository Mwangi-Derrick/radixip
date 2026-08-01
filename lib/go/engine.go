package radixip

import (
	"net"
	"runtime"
	"sync"
	"sync/atomic"
)

// STANDARD ENGINE

type StandardEngine struct {
	tree      RouteTree
	size      int64
	mu        sync.RWMutex
	statsData EngineStats
}

func NewStandardEngine(tree RouteTree) *StandardEngine {
	return &StandardEngine{
		tree:      tree,
		statsData: EngineStats{},
	}
}

func (e *StandardEngine) Insert(prefix *net.IPNet, metadata Metadata) error {
	ipnet := IpNetwork{IP: prefix.IP, Mask: prefix.Mask}
	isNew, err := e.tree.Insert(ipnet, metadata)
	if err != nil {
		return err
	}
	if isNew {
		atomic.AddInt64(&e.size, 1)
	}
	e.mu.Lock()
	e.statsData.Inserts++
	e.statsData.Size = int(atomic.LoadInt64(&e.size))
	e.mu.Unlock()
	return nil
}

func (e *StandardEngine) Lookup(ip net.IP) *Metadata {
	result := e.tree.Lookup(&ip)
	e.mu.Lock()
	e.statsData.Lookups++
	if result != nil {
		e.statsData.Hits++
	} else {
		e.statsData.Misses++
	}
	e.mu.Unlock()
	return result
}

func (e *StandardEngine) Remove(prefix *net.IPNet) *Metadata {
	ipnet := IpNetwork{IP: prefix.IP, Mask: prefix.Mask}
	removed := e.tree.Remove(&ipnet)
	if removed != nil {
		atomic.AddInt64(&e.size, -1)
		e.mu.Lock()
		e.statsData.Removals++
		e.statsData.Size = int(atomic.LoadInt64(&e.size))
		e.mu.Unlock()
	}
	return removed
}

func (e *StandardEngine) Contains(prefix *net.IPNet) bool {
	ipnet := IpNetwork{IP: prefix.IP, Mask: prefix.Mask}
	return e.tree.Contains(&ipnet)
}

func (e *StandardEngine) Clear() {
	e.tree.Clear()
	atomic.StoreInt64(&e.size, 0)
	e.mu.Lock()
	e.statsData.Size = 0
	e.mu.Unlock()
}

func (e *StandardEngine) Size() int {
	return int(atomic.LoadInt64(&e.size))
}

func (e *StandardEngine) Stats() *EngineStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s := e.statsData
	s.Size = int(atomic.LoadInt64(&e.size))
	return &s
}

// SHARDED ENGINE
// throughput = num_shards * throughput_per_shard

type ShardedEngine struct {
	shards    []*StandardEngine
	numShards int
	maskBits  int
}

func NewShardedEngine(numShards int, nodeVariant NodeVariant) *ShardedEngine {
	return NewShardedEngineWithTree(numShards, func() RouteTree {
		return NewUncompressedTree(nodeVariant)
	})
}

// NewShardedEngineWithTree accepts a factory so each shard can own a separate tree instance.
func NewShardedEngineWithTree(numShards int, treeFn func() RouteTree) *ShardedEngine {
	shards := make([]*StandardEngine, numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = NewStandardEngine(treeFn())
	}
	return &ShardedEngine{shards: shards, numShards: numShards, maskBits: 24}
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
		// Clear out the host bits based on the matching CIDR mask
		var mask uint32 = 0xFFFFFFFF
		if e.maskBits < 32 {
			mask = mask << (32 - e.maskBits)
		}
		// Modulo the masked network identifier
		/*
					When you perform hash & mask, you zero out (flush) the host bits at the end of the IP address.This operation works because a bitwise & (AND) acts like a filter:Any Bit & 1 = Any Bit (The bit stays exactly the same)Any Bit & 0 = 0 (The bit is completely flushed/cleared)Visual example with an IP addressLet's look at what happens when your /24 mask (which has eight 0s at the end) meets the IP address:textIP Address:   11000000 10101000 00000001 00110010  (192.168.1.50)
			Mask (&):     11111111 11111111 11111111 00000000  (255.255.255.0)
			-------------------------------------------------
			Result:       11000000 10101000 00000001 00000000  (192.168.1.0)
			                                         ^^^^^^^^
			                                    Flushed to zeros
			Use code with caution.Because the mask ends in all zeros, it forces the last 8 bits of the IP address to become 0, leaving you with the pure network prefix.
		*/
		maskedHash := hash & uint64(mask)

		return int(uint64(maskedHash) % uint64(e.numShards))
	default:
		// IPv6 Implementation (Using the same strategy to maintain performance)
		ip6 := ip.To16()
		high := uint64(ip6[0])<<56 | uint64(ip6[1])<<48 | uint64(ip6[2])<<40 | uint64(ip6[3])<<32 |
			uint64(ip6[4])<<24 | uint64(ip6[5])<<16 | uint64(ip6[6])<<8 | uint64(ip6[7])
		var mask6 uint64 = 0xFFFFFFFFFFFFFFFF
		if e.maskBits < 64 {
			mask6 = mask6 << (64 - e.maskBits)
		}
		hash = high & mask6
	}
	return int(hash % uint64(e.numShards))
}

func (e *ShardedEngine) Insert(prefix *net.IPNet, metadata Metadata) error {
	// Insert into all shards so any shard can serve lookups correctly
	for _, shard := range e.shards {
		if err := shard.Insert(prefix, metadata); err != nil {
			return err
		}
	}
	return nil
}

func (e *ShardedEngine) Lookup(ip net.IP) *Metadata {
	return e.shards[e.getShard(&ip)].Lookup(ip)
}

func (e *ShardedEngine) Remove(prefix *net.IPNet) *Metadata {
	var removed *Metadata
	for _, shard := range e.shards {
		if r := shard.Remove(prefix); removed == nil {
			removed = r
		}
	}
	return removed
}

func (e *ShardedEngine) Contains(prefix *net.IPNet) bool {
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

func (e *ShardedEngine) Size() int {
	if len(e.shards) > 0 {
		return e.shards[0].Size()
	}
	return 0
}

func (e *ShardedEngine) Stats() *EngineStats {
	var total EngineStats
	for _, shard := range e.shards {
		s := shard.Stats()
		total.Lookups += s.Lookups
		total.Hits += s.Hits
		total.Misses += s.Misses
	}
	if len(e.shards) > 0 {
		s := e.shards[0].Stats()
		total.Inserts = s.Inserts
		total.Removals = s.Removals
	}
	total.Size = e.Size()
	return &total
}

// ENGINE WRAPPER
// Single entry-point that dispatches to the right engine+tree combination.

type EngineWrapper struct {
	engine  RadixEngine
	variant EngineVariant
}

// NewEngineWrapper creates an engine using the UncompressedTree (default).
func NewEngineWrapper(variant EngineVariant, nodeVariant NodeVariant) *EngineWrapper {
	return NewEngineWrapperWithTree(variant, nodeVariant, false)
}

// NewEngineWrapperWithTree allows choosing compressed vs uncompressed tree at construction.
// compressed=true uses CompressedTree (Patricia); compressed=false uses UncompressedTree (bitwise trie).
func NewEngineWrapperWithTree(variant EngineVariant, nodeVariant NodeVariant, compressed bool) *EngineWrapper {
	treeFn := func() RouteTree {
		if compressed {
			return NewCompressedTree(nodeVariant)
		}
		return NewUncompressedTree(nodeVariant)
	}

	var engine RadixEngine
	switch variant {
	case EngineStandard:
		engine = NewStandardEngine(treeFn())
	case EngineConcurrent:
		engine = NewShardedEngineWithTree(16, treeFn)
	case EngineLockFree:
		// LockFree uses atomic nodes; uncompressed for simplicity
		engine = NewStandardEngine(NewUncompressedTree(NodeLockFree))
	case EngineAdaptive:
		cpus := runtime.NumCPU()
		if cpus > 4 {
			engine = NewShardedEngineWithTree(cpus*2, treeFn)
		} else {
			engine = NewStandardEngine(treeFn())
		}
	case EngineART:
		engine = NewARTEngineAdapter()
	default:
		engine = NewStandardEngine(treeFn())
	}

	return &EngineWrapper{engine: engine, variant: variant}
}

func (e *EngineWrapper) Insert(prefix *net.IPNet, metadata Metadata) error {
	return e.engine.Insert(prefix, metadata)
}

func (e *EngineWrapper) Lookup(ip net.IP) *Metadata {
	return e.engine.Lookup(ip)
}

func (e *EngineWrapper) Remove(prefix *net.IPNet) *Metadata {
	return e.engine.Remove(prefix)
}

func (e *EngineWrapper) Contains(prefix *net.IPNet) bool {
	return e.engine.Contains(prefix)
}

func (e *EngineWrapper) Clear() {
	e.engine.Clear()
}

func (e *EngineWrapper) Size() int {
	return e.engine.Size()
}

func (e *EngineWrapper) Stats() *EngineStats {
	stats := e.engine.Stats()
	return stats
}
