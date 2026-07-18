package go

import (
	"net"
	"net/ip"
	"sync/atomic"
	"unsafe"
	"runtime"
	"fmt"
)

type StandardEngine struct {
	root        RadixNode
	size        int64
	stats       sync.RWMutex
	statsData   EngineStats
	nodeBuilder *NodeBuilder
}

func NewStandardEngine(nodeVariant NodeVariant) *StandardEngine {
	builder := NewNodeBuilder(nodeVariant)
	return &StandardEngine{
		root:        builder.Build(),
		size:        0,
		statsData:   EngineStats{},
		nodeBuilder: builder,
	}
}

func (e *StandardEngine) Insert(prefix IpNetwork, metadata Metadata) error {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := e.root

	for depth := 0; depth < prefixLen; depth++ {
		bit := getBit(ip, depth)
		var next RadixNode
		if bit == 0 {
			next = current.Left()
		} else {
			next = current.Right()
		}

		if next != nil {
			current = next
		} else {
			newNode := e.nodeBuilder.Build()
			if bit == 0 {
				current.SetLeft(newNode)
			} else {
				current.SetRight(newNode)
			}
			current = newNode
		}
	}

	isNew := current.Metadata() == nil
	
	netPrefix := net.IPNet{IP: prefix.IP, Mask: prefix.Mask}
	current.SetPrefix(&netPrefix)
	current.SetMetadata(&metadata)

	if isNew {
		atomic.AddInt64(&e.size, 1)
	}

	e.stats.Lock()
	e.statsData.Inserts++
	e.statsData.Size = e.Size()
	e.stats.Unlock()

	return nil
}

func (e *StandardEngine) Lookup(ip *net.IP) *Metadata {
	var bestMatch *Metadata
	current := e.root
	depth := 0

	for current != nil {
		if p := current.Prefix(); p != nil {
			if p.Contains(*ip) {
				bestMatch = current.Metadata()
			}
		}

		bit := getBit(*ip, depth)
		if bit == 0 {
			current = current.Left()
		} else {
			current = current.Right()
		}
		depth++
	}

	e.stats.Lock()
	e.statsData.Lookups++
	if bestMatch != nil {
		e.statsData.Hits++
	} else {
		e.statsData.Misses++
	}
	e.stats.Unlock()

	return bestMatch
}

func (e *StandardEngine) Remove(prefix *IpNetwork) *Metadata {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := e.root
	for depth := 0; depth < prefixLen; depth++ {
		bit := getBit(ip, depth)
		if bit == 0 {
			current = current.Left()
		} else {
			current = current.Right()
		}
		if current == nil {
			return nil
		}
	}

	removed := current.Metadata()
	if removed != nil {
		current.ClearMetadata()
		atomic.AddInt64(&e.size, -1)
		
		e.stats.Lock()
		e.statsData.Removals++
		e.statsData.Size = e.Size()
		e.stats.Unlock()
	}

	return removed
}

func (e *StandardEngine) Contains(prefix *IpNetwork) bool {
	ip := prefix.IP
	ones, _ := prefix.Mask.Size()
	prefixLen := ones

	current := e.root
	for depth := 0; depth < prefixLen; depth++ {
		bit := getBit(ip, depth)
		if bit == 0 {
			current = current.Left()
		} else {
			current = current.Right()
		}
		if current == nil {
			return false
		}
	}
	return current.Metadata() != nil
}

func (e *StandardEngine) Clear() {
	e.root.SetLeft(nil)
	e.root.SetRight(nil)

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


func (e *ShardedEngine) getShard(ip *net.IP, maskBits int) int {
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
		if maskBits < 32 {
			mask = mask << (32 - maskBits)
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
		return int(uint64(hash & mask) % uint64(e.numShards))
	default:
// IPv6 Implementation (Using the same strategy to maintain performance)
	ip6 := ip.To16()
	high := uint64(ip6[0])<<56 | uint64(ip6[1])<<48 | uint64(ip6[2])<<40 | uint64(ip6[3])<<32 |
	        uint64(ip6[4])<<24 | uint64(ip6[5])<<16 | uint64(ip6[6])<<8  | uint64(ip6[7])
	
	// Apply IPv6 prefix mask (usually capped at /64 for network routing)
	var mask6 uint64 = 0xFFFFFFFFFFFFFFFF
	//because ipv6 are in 8 hectets each 64 bits or 16 bytes...and 16*8 = 128 bits
	if maskBits < 64 {
		mask6 = mask6 << (64 - maskBits)
	}
}
	return int((high & mask6) % uint64(e.numShards))
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
	//todo: let user to customize the network cidr mask(second parameter to this function)
	shardIdx := e.getShard(ip,24)
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

func (e *EngineWrapper) Insert(prefix IpNetwork, metadata Metadata) error {
	switch e.variant {
	case EngineVariantStandard, EngineVariantLockFree:
		return e.standard.Insert(prefix, metadata)
	case EngineVariantConcurrent:
		return e.concurrent.Insert(prefix, metadata)
	default:
		return e.standard.Insert(prefix, metadata)
	}
}

func (e *EngineWrapper) Lookup(ip *net.IP) *Metadata {
	switch e.variant {
	case EngineVariantStandard, EngineVariantLockFree:
		return e.standard.Lookup(ip)
	case EngineVariantConcurrent:
		return e.concurrent.Lookup(ip)
	default:
		return e.standard.Lookup(ip)
	}
}

func (e *EngineWrapper) Remove(prefix *IpNetwork) *Metadata {
	switch e.variant {
	case EngineVariantStandard, EngineVariantLockFree:
		return e.standard.Remove(prefix)
	case EngineVariantConcurrent:
		return e.concurrent.Remove(prefix)
	default:
		return e.standard.Remove(prefix)
	}
}

func (e *EngineWrapper) Contains(prefix *IpNetwork) bool {
	switch e.variant {
	case EngineVariantStandard, EngineVariantLockFree:
		return e.standard.Contains(prefix)
	case EngineVariantConcurrent:
		return e.concurrent.Contains(prefix)
	default:
		return e.standard.Contains(prefix)
	}
}

func (e *EngineWrapper) Clear() {
	switch e.variant {
	case EngineVariantStandard, EngineVariantLockFree:
		e.standard.Clear()
	case EngineVariantConcurrent:
		e.concurrent.Clear()
	default:
		e.standard.Clear()
	}
}

func (e *EngineWrapper) Size() int64 {
	switch e.variant {
	case EngineVariantStandard, EngineVariantLockFree:
		return e.standard.Size()
	case EngineVariantConcurrent:
		return e.concurrent.Size()
	default:
		return e.standard.Size()
	}
}

func (e *EngineWrapper) Stats() EngineStats {
	switch e.variant {
	case EngineVariantStandard, EngineVariantLockFree:
		return e.standard.Stats()
	case EngineVariantConcurrent:
		return e.concurrent.Stats()
	default:
		return e.standard.Stats()
	}
}

// Helper functions and types
func getNumCPU() int {
	// Get the number of available logical CPUs
	cpus := runtime.NumCPU()
	fmt.Printf("Number of logical CPUs: %d\n", cpus)
}