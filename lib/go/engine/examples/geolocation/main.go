package main

import (
	//"context"
	"fmt"
	"log"
	"net"
	"time"

	radixip "github.com/Mwangi-Derrick/radixip/lib/go"
)

func main() {
	fmt.Println("--- RadixIP Split-Plane Geolocation Example ---")

	// 1. Configure the architecture
	config := radixip.DefaultRadixConfig()
	config.EnableSplitPlane = true
	// Control Plane: Uncompressed tree for O(prefix_len) inserts
	config.WriteCompressed = false
	// Data Plane: Compressed tree for memory efficiency and O(k) lookups
	config.ReadCompressed = true

	// Optional: Configure Redis to sync the planes across a cluster
	// (We leave it nil here to demonstrate the local split-plane sync)
	config.Redis = nil

	// 2. Initialize the Hybrid Engine
	engine, err := radixip.NewHybridEngine(config)
	if err != nil {
		log.Fatalf("Failed to initialize engine: %v", err)
	}

	// 3. Start the sync loop (if Redis was configured)
	// ctx, cancel := context.WithCancel(context.Background())
	// defer cancel()
	// go engine.StartSync(ctx)

	// 4. Insert some geolocation data (goes to Control Plane)
	fmt.Println("Populating routing table...")

	prefixes := []struct {
		cidr    string
		country string
		region  string
	}{
		{"192.168.1.0/24", "US", "California"},
		{"192.168.1.128/25", "US", "Nevada"},
		{"10.0.0.0/8", "EU", "Germany"},
		{"10.10.0.0/16", "EU", "France"},
	}

	for _, p := range prefixes {
		_, ipNet, _ := net.ParseCIDR(p.cidr)
		meta := radixip.Metadata{
			Attributes: map[string]string{
				"country": p.country,
				"region":  p.region,
			},
		}

		err := engine.Insert(ipNet, meta)
		if err != nil {
			log.Printf("Failed to insert %s: %v", p.cidr, err)
		}
	}

	// Wait briefly if we were using Redis sync
	time.Sleep(50 * time.Millisecond)

	fmt.Printf("Engine size (Data Plane): %d\n\n", engine.Size())

	// 5. Perform lookups (goes to Data Plane)
	ipsToTest := []string{
		"192.168.1.50",  // Should match /24 (California)
		"192.168.1.150", // Should match /25 (Nevada)
		"10.10.5.5",     // Should match /16 (France)
		"10.20.5.5",     // Should match /8 (Germany)
		"8.8.8.8",       // Should not match
	}

	fmt.Println("Performing lookups...")
	for _, ipStr := range ipsToTest {
		ip := net.ParseIP(ipStr)
		start := time.Now()

		meta := engine.Lookup(ip)

		duration := time.Since(start)

		if meta != nil {
			data := meta.Attributes
			fmt.Printf("[ %s ] Found: %s - %s (%v)\n", ipStr, data["country"], data["region"], duration)
		} else {
			fmt.Printf("[ %s ] No match found (%v)\n", ipStr, duration)
		}
	}

	fmt.Println("\nSuccess! Split-Plane Architecture is operational.")
}
