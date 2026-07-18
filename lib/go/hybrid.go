package radixip

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
)

// HybridEngine acts as an orchestrator for a split-plane architecture.
// It routes writes to the controlPlane and reads to the dataPlane,
// synchronizing them via Redis.
type HybridEngine struct {
	controlPlane RadixEngine
	dataPlane    RadixEngine
	redis        *RedisClient
	channel      string
}

// NewHybridEngine creates a new HybridEngine
func NewHybridEngine(config *RadixConfig) (*HybridEngine, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required for HybridEngine")
	}

	controlPlane := NewEngineWrapperWithTree(config.EngineVariant, config.NodeVariant, config.WriteCompressed)
	dataPlane := NewEngineWrapperWithTree(config.EngineVariant, config.NodeVariant, config.ReadCompressed)

	var redisClient *RedisClient
	var err error
	if config.Redis != nil {
		redisClient, err = NewRedisClient(*config.Redis)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize redis client: %w", err)
		}
	}

	engine := &HybridEngine{
		controlPlane: controlPlane,
		dataPlane:    dataPlane,
		redis:        redisClient,
		channel:      config.RedisChannel,
	}

	// Boot-load the data plane from Redis if available
	if engine.redis != nil {
		ctx := context.Background()
		entries, err := engine.redis.HGetAll(ctx, "radixip:entries")
		if err == nil {
			for cidr, metaJSON := range entries {
				_, ipnet, err := net.ParseCIDR(cidr)
				if err == nil {
					var meta Metadata
					if json.Unmarshal([]byte(metaJSON), &meta) == nil {
						// Hydrate both planes
						engine.controlPlane.Insert(ipnet, meta)
						engine.dataPlane.Insert(ipnet, meta)
					}
				}
			}
		}
	}

	return engine, nil
}

// StartSync starts the Redis Pub/Sub synchronization loop for the data plane
func (e *HybridEngine) StartSync(ctx context.Context) error {
	if e.redis == nil {
		return fmt.Errorf("redis is not configured")
	}
	
	// Subscribe the dataPlane in a goroutine so it doesn't block
	go func() {
		err := e.redis.SubscribeEngineUpdates(ctx, e.channel, e.dataPlane)
		if err != nil {
			fmt.Printf("HybridEngine Redis sync stopped: %v\n", err)
		}
	}()
	
	return nil
}

// Insert adds a prefix with metadata to the control plane and publishes to Redis
func (e *HybridEngine) Insert(prefix *net.IPNet, metadata Metadata) error {
	// 1. Write to local control plane
	if err := e.controlPlane.Insert(prefix, metadata); err != nil {
		return err
	}

	// 2. Persist and Broadcast via Redis
	if e.redis != nil {
		ctx := context.Background()
		
		// Save state
		if jsonData, err := json.Marshal(metadata); err == nil {
			e.redis.HSet(ctx, "radixip:entries", prefix.String(), string(jsonData))
		}
		
		// Broadcast to all data planes (including our own local data plane)
		ipNetwork := IpNetwork{IP: prefix.IP, Mask: prefix.Mask}
		e.redis.PublishInsert(ctx, e.channel, ipNetwork, metadata)
	} else {
		// If no Redis, fallback to local sync
		e.dataPlane.Insert(prefix, metadata)
	}

	return nil
}

// Lookup queries the data plane exclusively
func (e *HybridEngine) Lookup(ip net.IP) *Metadata {
	return e.dataPlane.Lookup(ip)
}

// Remove deletes a prefix from the control plane and broadcasts to Redis
func (e *HybridEngine) Remove(prefix *net.IPNet) *Metadata {
	// 1. Remove from local control plane
	result := e.controlPlane.Remove(prefix)

	// 2. Remove and Broadcast via Redis
	if e.redis != nil {
		ctx := context.Background()
		e.redis.HDel(ctx, "radixip:entries", prefix.String())
		
		ipNetwork := IpNetwork{IP: prefix.IP, Mask: prefix.Mask}
		e.redis.PublishRemove(ctx, e.channel, ipNetwork)
	} else {
		// Fallback to local sync
		e.dataPlane.Remove(prefix)
	}

	return result
}

// Contains checks if a prefix exists in the data plane
func (e *HybridEngine) Contains(prefix *net.IPNet) bool {
	return e.dataPlane.Contains(prefix)
}

// Clear removes all entries and clears both planes
func (e *HybridEngine) Clear() {
	e.controlPlane.Clear()
	if e.redis != nil {
		ctx := context.Background()
		e.redis.PublishClear(ctx, e.channel)
		// We'd probably need a Lua script to clear the hash if we wanted to be perfectly clean
	} else {
		e.dataPlane.Clear()
	}
}

// Size returns the number of entries in the data plane
func (e *HybridEngine) Size() int {
	return e.dataPlane.Size()
}

// Stats aggregates statistics
func (e *HybridEngine) Stats() *EngineStats {
	// Return the stats of the data plane as it handles the reads
	return e.dataPlane.Stats()
}
