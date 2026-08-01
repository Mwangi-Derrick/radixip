// engine_art.go
package radixip

import (
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