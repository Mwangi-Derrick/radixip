package main

import (
	"fmt"
	"log"
	"net"

	radixIp "github.com/Mwangi-Derrick/radixip/lib/go/engine"
)

func main() {
	fmt.Println("--- RadixIP Basic Usage Example ---")

	// 1. Initialize the Engine
	// In this basic example, we use the standard Engine (control plane style)
	// It uses UncompressedTree by default, which is great for inserts/updates.
	tree := radixIp.NewUncompressedTree(radixIp.NormalTrieNode)
	engine := radixIp.NewStandardEngine(tree)
	if engine == nil {
		log.Fatalf("Failed to initialize engine")
	}

	// 2. Insert some IP prefixes with metadata
	// Prefixes are added from most specific to least specific (or vice versa),
	// but the tree handles finding the best match.
	prefixes := []struct {
		cidr string
		data map[string]interface{}
	}{
		{"192.168.1.0/24", map[string]interface{}{"owner": "Alice", "zone": "LAN"}},
		{"192.168.1.0/25", map[string]interface{}{"owner": "Bob", "zone": "LAN-subset"}},
		{"10.0.0.0/8", map[string]interface{}{"owner": "Corporate", "zone": "WAN"}},
	}

	fmt.Println("Inserting prefixes...")
	for _, p := range prefixes {
		// Parse the CIDR string into a net.IPNet
		_, ipNet, err := net.ParseCIDR(p.cidr)
		if err != nil {
			log.Printf("Invalid CIDR %s: %v", p.cidr, err)
			continue
		}

		// Create metadata
		meta := radixIp.Metadata{
			Value:      p.cidr,
			Attributes: toStringMap(p.data),
		}

		// Insert into the engine
		err = engine.Insert(ipNet, meta)
		if err != nil {
			log.Printf("Failed to insert %s: %v", p.cidr, err)
		}
	}

	fmt.Printf("Engine size: %d prefixes\n\n", engine.Size())

	// 3. Query IPs (Lookup)
	ipToTest := []string{
		"192.168.1.10",  // Should match Alice (/24) or Bob (/25)?
		"192.168.1.150", // Should match Bob (/25)
		"10.10.20.30",   // Should match Corporate (/8)
		"8.8.8.8",       // No match
	}

	fmt.Println("Performing lookups...")
	for _, ipStr := range ipToTest {
		ip := net.ParseIP(ipStr)

		// Perform lookup
		result := engine.Lookup(ip)

		if result != nil {
			fmt.Printf("-> %s: Matched %v (Data: %v)\n",
				ipStr, result.Attributes, result.Value)
		} else {
			fmt.Printf("-> %s: No match\n", ipStr)
		}
	}

	fmt.Println("\nSuccess!")
}

func toStringMap(data map[string]interface{}) map[string]string {
	// make a slice
	result := make(map[string]string)
	for key, value := range data {
		result[key] = value.(string)
	}
	return result
}
