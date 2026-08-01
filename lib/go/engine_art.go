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
type metadataStore struct {
	mu    sync.Mutex
	store map[netip.Addr]*Metadata
}

func newMetadataStore() *metadataStore {
	return &metadataStore{store: make(map[netip.Addr]*Metadata)}
}

func (s *metadataStore) set(addr netip.Addr, m *Metadata) {
	s.mu.Lock()
	s.store[addr] = m
	s.mu.Unlock()
}

func (s *metadataStore) get(addr netip.Addr) *Metadata {
	s.mu.Lock()
	m := s.store[addr]
	s.mu.Unlock()
	return m
}

func (s *metadataStore) del(addr netip.Addr) *Metadata {
	s.mu.Lock()
	m := s.store[addr]
	delete(s.store, addr)
	s.mu.Unlock()
	return m
}

func (s *metadataStore) clear() {
	s.mu.Lock()
	s.store = make(map[netip.Addr]*Metadata)
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

	// Heap-allocate and pin in the side map so the GC never frees it.
	m := new(Metadata)
	*m = metadata
	a.metas.set(p.Addr(), m)

	a.tree.Insert(p.Addr(), unsafe.Pointer(m))
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
	// Retrieve the old metadata before deletion
	old := a.metas.del(p.Addr())
	a.tree.Delete(p.Addr())
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
