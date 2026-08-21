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

func (a *ARTEngineAdapter) Stats() *EngineStats { return &EngineStats{} }


type ShardedARTEngineAdapter struct {
	tree  *art.Tree
	metas *metadataStore	
}

func NewShardedARTEngineAdapter() *ShardedARTEngineAdapter {
	return &ShardedARTEngineAdapter{
		tree:  art.NewTree(),
		metas: newMetadataStore(),
	}
}