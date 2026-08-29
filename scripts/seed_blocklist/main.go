package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
)

var rng = rand.New(rand.NewSource(42))

func randomCIDR() string {
	// Generate a random /24 to /32
	base := rng.Uint32()
	prefix := rng.Intn(9) + 24
	return fmt.Sprintf("%d.%d.%d.%d/%d",
		(base>>24)&0xFF, (base>>16)&0xFF, (base>>8)&0xFF, base&0xFF, prefix)
}

func main() {
	count := flag.Int("count", 100000, "Number of random CIDRs to seed")
	addr := flag.String("addr", "localhost:8082", "Address of the testapp seed endpoint")
	flag.Parse()

	log.Printf("Generating %d random CIDRs...", *count)

	var cidrs []string
	for i := 0; i < *count; i++ {
		cidrs = append(cidrs, randomCIDR())
	}

	url := fmt.Sprintf("http://%s/seed", *addr)
	log.Printf("Sending to %s in batches...", url)

	batchSize := 10000
	for i := 0; i < len(cidrs); i += batchSize {
		end := i + batchSize
		if end > len(cidrs) {
			end = len(cidrs)
		}
		batch := cidrs[i:end]

		reqBody, _ := json.Marshal(map[string]interface{}{
			"cidrs": batch,
		})

		resp, err := http.Post(url, "application/json", bytes.NewReader(reqBody))
		if err != nil {
			log.Fatalf("Failed to seed batch: %v", err)
		}
		resp.Body.Close()

		log.Printf("Seeded %d / %d", end, *count)
	}

	log.Printf("Successfully seeded %d CIDRs.", *count)
}
