// engine_art.go — ARTEngineAdapter wires the art.Tree into the RadixEngine interface.
package radixip

import (
	"net"
	"net/netip"
	"sync"
	"unsafe"

	"github.com/Mwangi-Derrick/radixip/lib/go/art"
)

// metadataStore keeps *Metadata values alive so the GC never collects
// something the ART tree points to via unsafe.Pointer.
// It is keyed on netip.Prefix (not just Addr) so that two different
// prefix lengths on the same network address (e.g. /24 and /25) are
// treated as distinct entries.
type metadataStore struct {
	mu    sync.Mutex
	store map[netip.Prefix]*Metadata
}

func newMetadataStore() *metadataStore {
	return &metadataStore{store: make(map[netip.Prefix]*Metadata)}
}

func (s *metadataStore) set(pfx netip.Prefix, m *Metadata) {
	s.mu.Lock()
	s.store[pfx] = m
	s.mu.Unlock()
}

func (s *metadataStore) del(pfx netip.Prefix) *Metadata {
	s.mu.Lock()
	m := s.store[pfx]
	delete(s.store, pfx)
	s.mu.Unlock()
	return m
}

func (s *metadataStore) clear() {
	s.mu.Lock()
	s.store = make(map[netip.Prefix]*Metadata)
	s.mu.Unlock()
}

func (s *metadataStore) size() int {
	s.mu.Lock()
	n := len(s.store)
	s.mu.Unlock()
	return n
}

// ---------------------------------------------------------------------------
// ARTEngineAdapter — implements RadixEngine
// ---------------------------------------------------------------------------

// ARTEngineAdapter wraps art.Tree and satisfies the RadixEngine interface.
// It stores *Metadata values in a side map so the Go GC never collects them
// while the ART tree holds raw unsafe.Pointer references.
type ARTEngineAdapter struct {
	tree  *art.Tree
	metas *metadataStore
}

// NewARTEngineAdapter returns a RadixEngine backed by the Adaptive Radix Tree.
func NewARTEngineAdapter() *ARTEngineAdapter {
	return &ARTEngineAdapter{
		tree:  art.NewTree(),
		metas: newMetadataStore(),
	}
}

func (a *ARTEngineAdapter) Insert(prefix *net.IPNet, metadata Metadata) error {
	p, err := netip.ParsePrefix(prefix.String())
	if err != nil {
		return err
	}
	p = p.Masked() // canonical form: zero host bits
	prefixLen := uint8(p.Bits())

	// Heap-allocate and GC-pin in the side map keyed by the full prefix
	// (Addr + length) so /24 and /25 on the same base address don't collide.
	m := new(Metadata)
	*m = metadata
	a.metas.set(p, m)

	a.tree.InsertPrefix(p.Addr(), prefixLen, unsafe.Pointer(m))
	return nil
}

func (a *ARTEngineAdapter) Lookup(ip net.IP) *Metadata {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return nil
	}
	addr = addr.Unmap() // normalise IPv4-mapped IPv6 → IPv4

	val, found := a.tree.Match(addr)
	if !found || val == nil {
		return nil
	}
	return (*Metadata)(val)
}

func (a *ARTEngineAdapter) Remove(prefix *net.IPNet) *Metadata {
	p, err := netip.ParsePrefix(prefix.String())
	if err != nil {
		return nil
	}
	p = p.Masked()
	// Retrieve the old metadata before deletion, keyed by full prefix.
	old := a.metas.del(p)
	a.tree.DeletePrefix(p.Addr(), uint8(p.Bits()))
	return old
}

func (a *ARTEngineAdapter) Contains(prefix *net.IPNet) bool {
	p, err := netip.ParsePrefix(prefix.String())
	if err != nil {
		return false
	}
	_, found := a.tree.Match(p.Addr())
	return found
}

func (a *ARTEngineAdapter) Clear() {
	a.tree = art.NewTree()
	a.metas.clear()
}

func (a *ARTEngineAdapter) Size() int { return a.metas.size() }

func (a *ARTEngineAdapter) Stats() *EngineStats {
	return &EngineStats{}
}

type ShardedARTEngineAdapter struct {
	trees     []*ARTEngineAdapter
	numShards int
	maskBits  int
}

func NewShardedARTEngineAdapter(numShards int, maskBits int) *ShardedARTEngineAdapter {
	shards := make([]*ARTEngineAdapter, numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = NewARTEngineAdapter()
	}
	return &ShardedARTEngineAdapter{
		trees:     shards,
		numShards: numShards,
		maskBits:  maskBits,
	}
}

func (e *ShardedARTEngineAdapter) getShard(ip *net.IP) int {
	var hash uint64
	switch {
	case ip.To4() != nil:
		ip4 := ip.To4()
		hash = uint64(uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3]))
		// Clear out the host bits based on the matching CIDR mask
		var mask uint32 = 0xFFFFFFFF
		if e.maskBits < 32 {
			mask = mask << (32 - e.maskBits)
		}
		// Modulo the masked network identifier
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

func (a *ShardedARTEngineAdapter) Insert(prefix *net.IPNet, metadata Metadata) error {
	// Insert into all shards so any shard can serve lookups correctly
	for _, shard := range a.trees {
		if err := shard.Insert(prefix, metadata); err != nil {
			return err
		}
	}
	return nil
}

func (a *ShardedARTEngineAdapter) Lookup(ip net.IP) *Metadata {
	return a.trees[a.getShard(&ip)].Lookup(ip)
}

func (a *ShardedARTEngineAdapter) Remove(prefix *net.IPNet) *Metadata {
	var removed *Metadata
	for _, shard := range a.trees {
		if r := shard.Remove(prefix); removed == nil {
			removed = r
		}
	}
	return removed
}

func (a *ShardedARTEngineAdapter) Contains(prefix *net.IPNet) bool {
	for _, shard := range a.trees {
		if shard.Contains(prefix) {
			return true
		}
	}
	return false
}

func (a *ShardedARTEngineAdapter) Size() int {
	// iterate over each shard/tree
	size := 0
	for _, shard := range a.trees {
		size += shard.Size()
		return size
	}
	return size
}

func (a *ShardedARTEngineAdapter) Stats() *EngineStats {
	return &EngineStats{}
}

func (a *ShardedARTEngineAdapter) Clear() {
	for _, shard := range a.trees {
		shard.Clear()
	}
}
