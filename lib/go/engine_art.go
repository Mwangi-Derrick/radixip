// engine_art.go
package radixip

import (
	"net"
	"net/netip"
	"unsafe"

	"github.com/Mwangi-Derrick/radixip/lib/go/art"
)

type ARTEngine struct {
	tree *art.Tree
}

func NewARTEngine() *ARTEngine {
	return &ARTEngine{
		tree: art.NewTree(),
	}
}

func (e *ARTEngine) Insert(cidr string, metadata interface{}) error {
	// Parse CIDR
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return err
	}

	// Store metadata
	metaPtr := unsafe.Pointer(&metadata)

	// Insert the network address
	e.tree.Insert(prefix.Addr(), metaPtr)
	return nil
}

func (e *ARTEngine) Match(ip netip.Addr) (interface{}, bool) {
	val, found := e.tree.Match(ip)
	if !found {
		return nil, false
	}
	return *(*interface{})(val), true
}

func (e *ARTEngine) Delete(cidr string) bool {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}
	return e.tree.Delete(prefix.Addr())
}

func (e *ARTEngine) Size() int {
	return e.tree.Size()
}

// Adapter to satisfy RadixEngine using the art.Tree implementation.
type ARTEngineAdapter struct {
	tree *art.Tree
}

func NewARTEngineAdapter() *ARTEngineAdapter {
	return &ARTEngineAdapter{tree: art.NewTree()}
}

func (a *ARTEngineAdapter) Insert(prefix *net.IPNet, metadata Metadata) error {
	// Convert to netip.Prefix
	p, err := netip.ParsePrefix(prefix.String())
	if err != nil {
		return err
	}
	// Ensure metadata is heap-allocated (address escapes)
	m := metadata
	a.tree.Insert(p.Addr(), unsafe.Pointer(&m))
	return nil
}

func (a *ARTEngineAdapter) Lookup(ip net.IP) *Metadata {
	addr, err := netip.ParseAddr(ip.String())
	if err != nil {
		return nil
	}
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
	if a.tree.Delete(p.Addr()) {
		return nil
	}
	return nil
}

func (a *ARTEngineAdapter) Contains(prefix *net.IPNet) bool {
	p, err := netip.ParsePrefix(prefix.String())
	if err != nil {
		return false
	}
	val, ok := a.tree.Match(p.Addr())
	return ok && val != nil
}

func (a *ARTEngineAdapter) Clear() { a.tree = art.NewTree() }

func (a *ARTEngineAdapter) Size() int { return a.tree.Size() }

func (a *ARTEngineAdapter) Stats() *EngineStats { return &EngineStats{} }
