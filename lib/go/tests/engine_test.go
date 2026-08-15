package tests

import (
	"fmt"
	"net"
	"testing"

	radixip "github.com/Mwangi-Derrick/radixip/lib/go"
)

func TestAllEngineAndNodeCombinations(t *testing.T) {
	engineVariants := []radixip.EngineVariant{
		radixip.EngineStandard,
		radixip.EngineConcurrent,
		radixip.EngineLockFree,
		radixip.EngineAdaptive,
	}

	nodeVariants := []radixip.NodeVariant{
		radixip.NormalTrieNode,
		radixip.AtomicTrieNode,
		radixip.PaddedTrieNode,
		radixip.LockFreeTrieNode,
		radixip.NormalRadixNode,
		radixip.AtomicRadixNode,
		radixip.PaddedRadixNode,
		radixip.LockFreeRadixNode,
	}

	for _, ev := range engineVariants {
		for _, nv := range nodeVariants {
			for _, compressed := range []bool{false, true} {
				testName := fmt.Sprintf("Engine_%s/Node_%s/Compressed_%v", ev, nv, compressed)
				t.Run(testName, func(t *testing.T) {
					e := radixip.NewEngineWrapperWithTree(ev, nv, compressed)
					if e == nil {
						t.Fatalf("Failed to initialize engine wrapper")
					}

					// Test empty state
					if e.Size() != 0 {
						t.Errorf("Expected size 0, got %d", e.Size())
					}

					// Test basic insert
					_, ipnet1, _ := net.ParseCIDR("10.0.0.0/8")
					err := e.Insert(ipnet1, radixip.Metadata{Value: "broad"})
					if err != nil {
						t.Fatalf("Failed to insert: %v", err)
					}

					_, ipnet2, _ := net.ParseCIDR("10.1.2.0/24")
					err = e.Insert(ipnet2, radixip.Metadata{Value: "specific"})
					if err != nil {
						t.Fatalf("Failed to insert specific: %v", err)
					}

					// Check contains
					if !e.Contains(ipnet1) {
						t.Errorf("Expected true for Contains broad prefix")
					}

					// Longest Prefix Match lookup hit
					ip := net.ParseIP("10.1.2.99")
					res := e.Lookup(ip)
					if res == nil || res.Value != "specific" {
						t.Errorf("Expected 'specific', got %v", res)
					}

					// Broad Match lookup hit
					ip2 := net.ParseIP("10.2.0.1")
					res2 := e.Lookup(ip2)
					if res2 == nil || res2.Value != "broad" {
						t.Errorf("Expected 'broad', got %v", res2)
					}

					// Lookup miss
					ip3 := net.ParseIP("192.168.1.1")
					res3 := e.Lookup(ip3)
					if res3 != nil {
						t.Errorf("Expected nil lookup, got %v", res3)
					}

					// Remove specific prefix
					removed := e.Remove(ipnet2)
					if removed == nil || removed.Value != "specific" {
						t.Errorf("Expected removed metadata to be 'specific'")
					}

					// Lookup should fall back to broad now
					resFall := e.Lookup(ip)
					if resFall == nil || resFall.Value != "broad" {
						t.Errorf("Expected fallback to 'broad' after specific removal, got %v", resFall)
					}

					// Clear tree
					e.Clear()
					if e.Size() != 0 {
						t.Errorf("Expected size 0 after clear, got %d", e.Size())
					}
				})
			}
		}
	}
}

func TestIPv6LongestPrefixMatch(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(fmt.Sprintf("Compressed_%v", compressed), func(t *testing.T) {
			e := radixip.NewEngineWrapperWithTree(radixip.EngineStandard, radixip.AtomicRadixNode, compressed)

			_, ipnet1, _ := net.ParseCIDR("2001:db8::/32")
			_, ipnet2, _ := net.ParseCIDR("2001:db8:85a3::/48")

			_ = e.Insert(ipnet1, radixip.Metadata{Value: "v6-broad"})
			_ = e.Insert(ipnet2, radixip.Metadata{Value: "v6-specific"})

			// Match specific
			ip := net.ParseIP("2001:db8:85a3:0000:0000:8a2e:0370:7334")
			res := e.Lookup(ip)
			if res == nil || res.Value != "v6-specific" {
				t.Errorf("Expected v6-specific, got %v", res)
			}

			// Match broad
			ip2 := net.ParseIP("2001:db8:9999::1")
			res2 := e.Lookup(ip2)
			if res2 == nil || res2.Value != "v6-broad" {
				t.Errorf("Expected v6-broad, got %v", res2)
			}
		})
	}
}
