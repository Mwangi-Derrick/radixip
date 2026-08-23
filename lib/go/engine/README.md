# RadixIP Go Engine

The Go implementation of RadixIP focuses on extreme performance, clocking in at ~45ns per lookup. It provides full support for the Split-Plane architecture (Hybrid Engine), Sharded scaling, and Redis-backed state synchronization.

## Installation

```bash
go get github.com/Mwangi-Derrick/radixip/lib/go
```

## Basic Usage (Data Plane / Caching Engine)

If you just need an ultra-fast in-memory cache with no Redis overhead:

```go
package main

import (
	"fmt"
	"net"
	"github.com/Mwangi-Derrick/radixip/lib/go"
)

func main() {
	// StandardEngine uses a single tree. NodeNormal is the standard Node implementation.
	engine, _ := radixip.NewStandardEngine(radixip.NodeNormal)

	// Insert
	_, cidr, _ := net.ParseCIDR("192.168.1.0/24")
	meta := radixip.Metadata{Data: map[string]interface{}{"zone": "LAN"}}
	engine.Insert(cidr, meta)

	// Lookup
	ip := net.ParseIP("192.168.1.100")
	if result := engine.Lookup(ip); result != nil {
		fmt.Printf("Match: %v\n", result.Data)
	}
}
```

## Advanced Usage: Hybrid Engine with Redis (DDoS Protection)

The Hybrid Engine connects a Control Plane (for fast concurrent writes) and a Data Plane (for sub-microsecond reads), syncing state instantly across all instances using Redis Pub/Sub.

```go
import "github.com/Mwangi-Derrick/radixip/lib/go"

func main() {
	// Initialize with Default Config (connects to Redis at localhost:6379)
	config := radixip.DefaultRadixConfig()
	config.Redis = &radixip.RedisConfig{Addr: "localhost:6379"}
	
	engine, err := radixip.NewHybridEngine(config)
	if err != nil {
		panic(err)
	}

	// Any insert here automatically publishes to Redis, so all other 
	// running Go instances instantly receive the update.
	_, badNet, _ := net.ParseCIDR("100.100.100.0/24")
	engine.Insert(badNet, radixip.Metadata{
		Data: map[string]interface{}{"reason": "DDoS"},
	})
}
```

## Benchmarks

Run the built-in Go benchmarks:
```bash
cd lib/go
go test -bench=. -benchmem ./...
```
