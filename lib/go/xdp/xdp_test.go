package xdp

import (
	"net/netip"
	"os"
	"testing"
)

const (
	xdpDrop = 1
	xdpPass = 2
)

// frame builds Ethernet + minimal IP header + padding. Source address bytes
// are placed exactly as on the wire.
func frame(src netip.Addr, vlan bool) []byte {
	b := make([]byte, 0, 96)
	b = append(b, make([]byte, 12)...) // dst + src MAC
	et := []byte{0x08, 0x00}
	if src.Is6() {
		et = []byte{0x86, 0xDD}
	}
	if vlan {
		b = append(b, 0x81, 0x00, 0x00, 0x01)
	}
	b = append(b, et...)
	if src.Is4() {
		ip := make([]byte, 20)
		ip[0] = 0x45
		s := src.As4()
		copy(ip[12:16], s[:])
		b = append(b, ip...)
	} else {
		ip := make([]byte, 40)
		ip[0] = 0x60
		s := src.As16()
		copy(ip[8:24], s[:])
		b = append(b, ip...)
	}
	return append(b, make([]byte, 32)...)
}

func newTestLoader(t *testing.T) *Loader {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root / CAP_BPF")
	}
	l, err := Load(Config{
		NeverBlock: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	}) // no PinDir: tests do not need bpffs
	if err != nil {
		t.Skipf("cannot load BPF here: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func run(t *testing.T, l *Loader, pkt []byte) uint32 {
	t.Helper()
	ret, _, err := l.objs.RadixipXdp.Test(pkt)
	if err != nil {
		t.Fatal(err)
	}
	return ret
}

func TestV4PrefixAndEndianness(t *testing.T) {
	l := newTestLoader(t)
	if err := l.Put(netip.MustParsePrefix("198.51.100.0/24")); err != nil {
		t.Fatal(err)
	}
	if got := run(t, l, frame(netip.MustParseAddr("198.51.100.7"), false)); got != xdpDrop {
		t.Errorf("inside prefix: got %d want DROP", got)
	}
	if got := run(t, l, frame(netip.MustParseAddr("198.51.101.7"), false)); got != xdpPass {
		t.Errorf("outside prefix: got %d want PASS", got)
	}
	// Byte-reversed address must NOT match; catches a from_be/endianness bug.
	if got := run(t, l, frame(netip.MustParseAddr("100.51.198.7"), false)); got != xdpPass {
		t.Errorf("reversed bytes: got %d want PASS", got)
	}
}

func TestV6(t *testing.T) {
	l := newTestLoader(t)
	if err := l.Put(netip.MustParsePrefix("2001:db8::/32")); err != nil {
		t.Fatal(err)
	}
	if got := run(t, l, frame(netip.MustParseAddr("2001:db8::1"), false)); got != xdpDrop {
		t.Errorf("v6 inside: got %d want DROP", got)
	}
	if got := run(t, l, frame(netip.MustParseAddr("2001:db9::1"), false)); got != xdpPass {
		t.Errorf("v6 outside: got %d want PASS", got)
	}
}

func TestVLANTagged(t *testing.T) {
	l := newTestLoader(t)
	_ = l.Put(netip.MustParsePrefix("198.51.100.0/24"))
	if got := run(t, l, frame(netip.MustParseAddr("198.51.100.7"), true)); got != xdpDrop {
		t.Errorf("vlan: got %d want DROP", got)
	}
}

func TestTruncatedPasses(t *testing.T) {
	l := newTestLoader(t)
	_ = l.Put(netip.MustParsePrefix("198.51.100.0/24"))
	pkt := frame(netip.MustParseAddr("198.51.100.7"), false)[:20] // cuts the IP header
	// BPF_PROG_TEST_RUN requires >= 14 bytes; 20 is allowed and still truncated.
	if got := run(t, l, pkt); got != xdpPass {
		t.Errorf("truncated: got %d want PASS", got)
	}
}

func TestGuard(t *testing.T) {
	l := newTestLoader(t)
	for _, bad := range []string{"0.0.0.0/0", "1.0.0.0/4", "10.1.0.0/16", "::/0"} {
		if err := l.Put(netip.MustParsePrefix(bad)); err == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
	// ::ffff:198.51.100.0/120 must land in the v4 map as 198.51.100.0/24.
	if err := l.Put(netip.MustParsePrefix("::ffff:198.51.100.0/120")); err != nil {
		t.Fatal(err)
	}
	if got := run(t, l, frame(netip.MustParseAddr("198.51.100.9"), false)); got != xdpDrop {
		t.Errorf("mapped entry: got %d want DROP", got)
	}
}