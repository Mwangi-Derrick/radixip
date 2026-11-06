// Package xdp loads the RadixIP XDP blocklist program and mirrors CIDRs into
// its LPM maps. Contract: load (create/reuse pinned maps) -> seed -> attach.
// Every failure is meant to be handled fail-open by the caller (log, keep
// serving userspace-only); see guide section 7.
package xdp

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

// Must match the C program and the Aya program.
type keyV4 struct {
	PrefixLen uint32
	Addr      [4]byte
}

type keyV6 struct {
	PrefixLen uint32
	Addr      [16]byte
}

type Config struct {
	Iface      string
	DriverMode bool           // false = generic/SKB (portable default for sidecars)
	PinDir     string         // e.g. /sys/fs/bpf/radixip/v1 ; "" disables pinning (tests)
	MinV4      int            // reject shorter prefixes (default 8)
	MinV6      int            // default 16
	NeverBlock []netip.Prefix // cluster CIDR, node IPs, health checks, admin range
}

type Stats struct {
	V4Drop, V4Pass, V6Drop, V6Pass, ParseErr uint64
}

type Loader struct {
	cfg          Config
	objs         struct {
		RadixipXdp  *ebpf.Program `ebpf:"radixip_xdp"`
		BlocklistV4 *ebpf.Map     `ebpf:"blocklist_v4"`
		BlocklistV6 *ebpf.Map     `ebpf:"blocklist_v6"`
		Stats       *ebpf.Map     `ebpf:"stats"`
	}
	lnk          link.Link
	PinnedReused bool
}

// Load creates (or reuses pinned) maps and loads the program. It does NOT attach.
func Load(cfg Config) (*Loader, error) {
	if cfg.MinV4 == 0 {
		cfg.MinV4 = 8
	}
	if cfg.MinV6 == 0 {
		cfg.MinV6 = 16
	}
	l := &Loader{cfg: cfg}
	opts := &ebpf.CollectionOptions{}
	pinned := map[string]*ebpf.Map{}

	if cfg.PinDir != "" {
		if err := os.MkdirAll(cfg.PinDir, 0o700); err != nil {
			return nil, fmt.Errorf("pin dir %s (is bpffs mounted?): %w", cfg.PinDir, err)
		}
		for _, name := range []string{"blocklist_v4", "blocklist_v6"} {
			m, err := ebpf.LoadPinnedMap(filepath.Join(cfg.PinDir, name), nil)
			if err == nil {
				pinned[name] = m
				l.PinnedReused = true
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("load pinned %s: %w", name, err)
			}
		}
		opts.MapReplacements = pinned
	}

	if err := loadXdpBlocklistObjects(&l.objs, opts); err != nil {
		return nil, fmt.Errorf("load objects (verifier/caps/pinned map mismatch?): %w", err)
	}

	if cfg.PinDir != "" {
		for name, m := range map[string]*ebpf.Map{
			"blocklist_v4": l.objs.BlocklistV4,
			"blocklist_v6": l.objs.BlocklistV6,
		} {
			if _, ok := pinned[name]; ok {
				continue
			}
			if err := m.Pin(filepath.Join(cfg.PinDir, name)); err != nil {
				l.Close()
				return nil, fmt.Errorf("pin %s: %w", name, err)
			}
		}
	}
	return l, nil
}

// Attach hooks the program to the interface. Call after Seed.
func (l *Loader) Attach() error {
	ifc, err := net.InterfaceByName(l.cfg.Iface)
	if err != nil {
		return err
	}
	flags := link.XDPGenericMode
	if l.cfg.DriverMode {
		flags = link.XDPDriverMode
	}
	lnk, err := link.AttachXDP(link.XDPOptions{
		Program:   l.objs.RadixipXdp,
		Interface: ifc.Index,
		Flags:     flags,
	})
	if err != nil {
		return fmt.Errorf("attach %s: %w", l.cfg.Iface, err)
	}
	l.lnk = lnk // not pinned: detaches if this process dies
	return nil
}

// Normalize applies the self-lockout guard (guide 7.7) and maps v4-in-v6 to v4.
func (l *Loader) Normalize(p netip.Prefix) (netip.Prefix, error) {
	p = p.Masked()
	if p.Addr().Is4In6() {
		if p.Bits() < 96 {
			return p, fmt.Errorf("%s: mapped-v6 prefix shorter than /96", p)
		}
		p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
	}
	min := l.cfg.MinV6
	if p.Addr().Is4() {
		min = l.cfg.MinV4
	}
	if p.Bits() < min {
		return p, fmt.Errorf("%s: prefix shorter than minimum /%d", p, min)
	}
	for _, nb := range l.cfg.NeverBlock {
		if p.Overlaps(nb) {
			return p, fmt.Errorf("%s: overlaps never-block entry %s", p, nb)
		}
	}
	return p, nil
}

func (l *Loader) Put(p netip.Prefix) error {
	p, err := l.Normalize(p)
	if err != nil {
		return err
	}
	if p.Addr().Is4() {
		return l.objs.BlocklistV4.Update(&keyV4{uint32(p.Bits()), p.Addr().As4()}, uint8(1), ebpf.UpdateAny)
	}
	return l.objs.BlocklistV6.Update(&keyV6{uint32(p.Bits()), p.Addr().As16()}, uint8(1), ebpf.UpdateAny)
}

func (l *Loader) Delete(p netip.Prefix) error {
	p, err := l.Normalize(p)
	if err != nil {
		return err
	}
	if p.Addr().Is4() {
		err = l.objs.BlocklistV4.Delete(&keyV4{uint32(p.Bits()), p.Addr().As4()})
	} else {
		err = l.objs.BlocklistV6.Delete(&keyV6{uint32(p.Bits()), p.Addr().As16()})
	}
	if errors.Is(err, ebpf.ErrKeyNotExist) {
		return nil
	}
	return err
}

// Seed inserts a snapshot. Rejected entries are collected, not fatal.
func (l *Loader) Seed(prefixes []netip.Prefix) (ok int, errs []error) {
	for _, p := range prefixes {
		if err := l.Put(p); err != nil {
			errs = append(errs, err)
			continue
		}
		ok++
	}
	return ok, errs
}

func (l *Loader) Stats() (Stats, error) {
	var out Stats
	dst := []*uint64{&out.V4Drop, &out.V4Pass, &out.V6Drop, &out.V6Pass, &out.ParseErr}
	for i := range dst {
		var per []uint64
		if err := l.objs.Stats.Lookup(uint32(i), &per); err != nil {
			return out, err
		}
		for _, v := range per {
			*dst[i] += v
		}
	}
	return out, nil
}

func (l *Loader) Close() error {
	if l.lnk != nil {
		l.lnk.Close()
	}
	return l.objs.Close()
}