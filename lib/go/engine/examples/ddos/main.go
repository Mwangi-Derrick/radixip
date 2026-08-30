package ddos

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	radixip "github.com/Mwangi-Derrick/radixip/lib/go/engine"
)

// Global threshold config for the DDoS protection engine
const (
	MaxAllowed = 20              // Max allowed packets per source IP in the sampling window
	WindowSize = 1 * time.Second // Time window for counting
)

type DDoSProtector struct {
	engine *radixip.HybridEngine

	// Internal tracking: maps source IP to their current counts in the window
	mu     sync.Mutex
	counts map[string]int
}

func NewDDoSProtector(e *radixip.HybridEngine) *DDoSProtector {
	return &DDoSProtector{
		engine: e,
		counts: make(map[string]int),
	}
}

// ProcessPacket simulates an incoming packet and decides whether to drop it
// Returns: isBlocked, metadata
func (d *DDoSProtector) ProcessPacket(srcIP net.IP) (bool, radixip.Metadata) {

	// 1. Check against the global routing table first (fast path)
	// This handles large known bad actors or geography-based rules
	match := d.engine.Lookup(srcIP)
	if match != nil {
		return true, *match
	}

	// 2. Apply sliding-window rate limiting (dynamic protection)
	srcStr := srcIP.String()

	// Lock to update counts safely
	d.mu.Lock()

	// Increment count for this source
	d.counts[srcStr]++

	// Check threshold immediately
	if d.counts[srcStr] > MaxAllowed {
		// BLOCKED: Drop the packet

		// Log for debugging (in production, you might just drop silently or send to a SIEM)
		log.Printf("BLOCKED: %s exceeded %d packets/sec threshold", srcStr, MaxAllowed)

		// Reset count so it can recover after window expires
		// Note: For a true sliding window, you'd use a timer to decrement after WindowSize.
		// For simplicity here, we reset on block, effectively creating a "burst" detector.
		d.counts[srcStr] = 0

		d.mu.Unlock()
		return true, radixip.Metadata{}
	}

	d.mu.Unlock()

	// Allow the packet
	return false, radixip.Metadata{}
}

// RefreshWindow resets all counters periodically
// In a real app, this might run via a background goroutine or cron job
func (d *DDoSProtector) RefreshWindow() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.counts = make(map[string]int)
	fmt.Println("Rate limit window refreshed.")
}

// InsertRoute allows dynamic updates to the routing table (e.g., a new BGP announcement)
func (d *DDoSProtector) InsertRoute(cidr *net.IPNet, metadata radixip.Metadata) error {
	return d.engine.Insert(cidr, metadata)
}

// Example of how to use the protector
func main() {
	fmt.Println("--- RadixIP DDoS Protection Demo ---")

	// 1. Initialize RadixIP Engine (Data Plane for speed)
	// We set compressed to true for fast lookups
	config := radixip.DefaultRadixConfig()
	config.ReadCompressed = true
	engine, err := radixip.NewHybridEngine(config)
	if err != nil {
		log.Fatal(err)
	}

	// 2. Populate with some static/global rules
	// Example: Blocking a known bad AS or region
	_, badNet, _ := net.ParseCIDR("100.100.100.0/24")
	engine.Insert(badNet, radixip.Metadata{
		Attributes: map[string]string{
			"reason": "Known malicious AS",
		},
	})

	protector := NewDDoSProtector(engine)

	// 3. Simulate Attack Traffic
	attackers := []string{
		"100.100.100.5", // Should be blocked by static rule
		"192.168.1.100", // Normal user
		"192.168.1.100", // Attacker 1 starts spamming
		"192.168.1.100",
		"192.168.1.100",
		"192.168.1.100",
		"192.168.1.100",
		"192.168.1.100",
		"192.168.1.100",
		"192.168.1.100",
	}

	fmt.Println("Simulating traffic...")
	for _, ipStr := range attackers {
		ip := net.ParseIP(ipStr)
		blocked, _ := protector.ProcessPacket(ip)
		if blocked {
			fmt.Printf("Drop  <-- %s (Blocked)\n", ipStr)
		} else {
			fmt.Printf("Allow --> %s (Allowed)\n", ipStr)
		}
		time.Sleep(50 * time.Millisecond)
	}

	fmt.Println("\nAttack stopped. Refreshing window...")
	protector.RefreshWindow()

	// 4. Show dynamic update capability
	// A new route is announced (e.g., new BGP(border gateway protocol) route)
	_, newNet, _ := net.ParseCIDR("10.10.0.0/16")
	protector.InsertRoute(newNet, radixip.Metadata{
		Attributes: map[string]string{
			"reason": "Emergency Traffic Lane",
		},
	})

	fmt.Println("\nQuerying new route...")
	meta := protector.engine.Lookup(net.ParseIP("10.10.5.5"))
	if meta != nil {
		fmt.Printf("Found: %v\n", meta.Attributes)
	}
}
