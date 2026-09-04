package art_test

import (
	"net/netip"
	"testing"
	"unsafe"

	"github.com/Mwangi-Derrick/radixip/lib/go/engine/art"
)

// helper: store a string value as unsafe.Pointer
func strPtr(s string) unsafe.Pointer {
	c := s // copy to heap
	return unsafe.Pointer(&c)
}

func ptrStr(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	return *(*string)(p)
}

func mustAddr(s string) netip.Addr {
	a, err := netip.ParseAddr(s)
	if err != nil {
		panic(err)
	}
	return a
}

// Basic correctness

func TestTree_InsertAndMatch(t *testing.T) {
	tree := art.NewTree()

	tree.Insert(mustAddr("10.0.0.1"), strPtr("first"))
	tree.Insert(mustAddr("10.0.0.2"), strPtr("second"))
	tree.Insert(mustAddr("192.168.1.1"), strPtr("third"))

	cases := []struct {
		ip   string
		want string
	}{
		{"10.0.0.1", "first"},
		{"10.0.0.2", "second"},
		{"192.168.1.1", "third"},
	}

	for _, tc := range cases {
		val, found := tree.Match(mustAddr(tc.ip))
		if !found {
			t.Errorf("Match(%s): expected found=true", tc.ip)
			continue
		}
		got := ptrStr(val)
		if got != tc.want {
			t.Errorf("Match(%s): got %q, want %q", tc.ip, got, tc.want)
		}
	}
}

func TestTree_Miss(t *testing.T) {
	tree := art.NewTree()
	tree.Insert(mustAddr("10.0.0.1"), strPtr("v"))

	_, found := tree.Match(mustAddr("10.0.0.2"))
	if found {
		t.Error("Match(10.0.0.2): expected miss, got hit")
	}
}

func TestTree_Overwrite(t *testing.T) {
	tree := art.NewTree()
	tree.Insert(mustAddr("10.0.0.1"), strPtr("old"))
	tree.Insert(mustAddr("10.0.0.1"), strPtr("new"))

	val, found := tree.Match(mustAddr("10.0.0.1"))
	if !found {
		t.Fatal("Match: expected found")
	}
	if got := ptrStr(val); got != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
	if tree.Size() != 1 {
		t.Errorf("Size: got %d, want 1", tree.Size())
	}
}

func TestTree_Delete(t *testing.T) {
	tree := art.NewTree()
	tree.Insert(mustAddr("10.0.0.1"), strPtr("v"))

	if !tree.Delete(mustAddr("10.0.0.1")) {
		t.Fatal("Delete returned false for existing key")
	}
	_, found := tree.Match(mustAddr("10.0.0.1"))
	if found {
		t.Error("Match after Delete: expected miss")
	}
	if tree.Size() != 0 {
		t.Errorf("Size after delete: got %d, want 0", tree.Size())
	}
}

func TestTree_DeleteMiss(t *testing.T) {
	tree := art.NewTree()
	if tree.Delete(mustAddr("99.99.99.99")) {
		t.Error("Delete of nonexistent key should return false")
	}
}

// Node grow/shrink transitions

// Force Node4 → Node16 → Node48 → Node256 by inserting 200+ distinct IPs
func TestTree_GrowTransitions(t *testing.T) {
	tree := art.NewTree()

	const n = 200
	for i := 0; i < n; i++ {
		ip := netip.AddrFrom4([4]byte{10, 0, 0, byte(i)})
		tree.Insert(ip, strPtr("v"))
	}

	if tree.Size() != n {
		t.Fatalf("Size: got %d, want %d", tree.Size(), n)
	}

	// All entries must still be retrievable
	for i := 0; i < n; i++ {
		ip := netip.AddrFrom4([4]byte{10, 0, 0, byte(i)})
		_, found := tree.Match(ip)
		if !found {
			t.Errorf("Match(10.0.0.%d): not found after grow", i)
		}
	}
}

// Force shrink: insert 60, then delete 50 (should shrink Node256→Node48→Node16→Node4)
func TestTree_ShrinkTransitions(t *testing.T) {
	tree := art.NewTree()

	const total = 60
	for i := 0; i < total; i++ {
		ip := netip.AddrFrom4([4]byte{10, 0, 0, byte(i)})
		tree.Insert(ip, strPtr("v"))
	}

	for i := 0; i < 50; i++ {
		ip := netip.AddrFrom4([4]byte{10, 0, 0, byte(i)})
		if !tree.Delete(ip) {
			t.Errorf("Delete(10.0.0.%d): expected true", i)
		}
	}

	if tree.Size() != 10 {
		t.Errorf("Size: got %d, want 10", tree.Size())
	}

	for i := 50; i < total; i++ {
		ip := netip.AddrFrom4([4]byte{10, 0, 0, byte(i)})
		_, found := tree.Match(ip)
		if !found {
			t.Errorf("Match(10.0.0.%d): expected found after partial delete", i)
		}
	}
}

// Concurrent access safety

func TestTree_ConcurrentInsertLookup(t *testing.T) {
	tree := art.NewTree()
	const workers = 8
	const perWorker = 100

	done := make(chan struct{})
	for w := 0; w < workers; w++ {
		go func(wid int) {
			for i := 0; i < perWorker; i++ {
				ip := netip.AddrFrom4([4]byte{byte(wid), byte(i >> 8), byte(i), 1})
				tree.Insert(ip, strPtr("v"))
				tree.Match(ip)
			}
			done <- struct{}{}
		}(w)
	}
	for i := 0; i < workers; i++ {
		<-done
	}
}

// Benchmarks

func BenchmarkTree_Insert(b *testing.B) {
	ips := make([]netip.Addr, 1000)
	for i := range ips {
		ips[i] = netip.AddrFrom4([4]byte{byte(i >> 8), byte(i), 0, 1})
	}

	b.ResetTimer()
	for b.Loop() {
		tree := art.NewTree()
		for _, ip := range ips {
			tree.Insert(ip, strPtr("v"))
		}
	}
}

func BenchmarkTree_Match_Hit(b *testing.B) {
	tree := art.NewTree()
	const n = 10_000
	ips := make([]netip.Addr, n)
	for i := 0; i < n; i++ {
		ip := netip.AddrFrom4([4]byte{10, byte(i >> 8), byte(i), 1})
		ips[i] = ip
		tree.Insert(ip, strPtr("v"))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range ips {
			tree.Match(ip)
		}
	}
}

func BenchmarkTree_Match_Miss(b *testing.B) {
	tree := art.NewTree()
	for i := 0; i < 10_000; i++ {
		ip := netip.AddrFrom4([4]byte{10, byte(i >> 8), byte(i), 1})
		tree.Insert(ip, strPtr("v"))
	}
	missIPs := make([]netip.Addr, 10_000)
	for i := range missIPs {
		missIPs[i] = netip.AddrFrom4([4]byte{172, byte(i >> 8), byte(i), 1})
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, ip := range missIPs {
			tree.Match(ip)
		}
	}
}
