package radixip

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"
)

// CacheConfig holds configuration for the cache
type CacheConfig struct {
	MaxEntries int
	TTLSeconds *uint64
}

// RadixCache manages caching between Redis and the engine
type RadixCache struct {
	cache   sync.RWMutex
	entries map[string]*cacheEntry // key is IP string
	config  CacheConfig
	engine  RadixEngine
	redis   *RedisClient
}

type cacheEntry struct {
	metadata  *Metadata
	expiresAt *time.Time
}

// NewRadixCache creates a new RadixCache instance
func NewRadixCache(config CacheConfig, engine RadixEngine, redisClient *RedisClient) *RadixCache {
	rc := &RadixCache{
		entries: make(map[string]*cacheEntry),
		config:  config,
		engine:  engine,
		redis:   redisClient,
	}

	// Boot-load prefixes from Redis
	if rc.redis != nil {
		ctx := context.Background()
		entries, err := rc.redis.HGetAll(ctx, "radixip:entries")
		if err == nil {
			for cidr, metaJSON := range entries {
				ipNet, _, err := net.ParseCIDR(cidr)
				if err == nil {
					network := IpNetwork{IP: ipNet, Mask: net.CIDRMask(24, 32)} // Simplify mask logic
					if _, ipnet, err := net.ParseCIDR(cidr); err == nil {
						network = IpNetwork{IP: ipnet.IP, Mask: ipnet.Mask}
					}
					var meta Metadata
					if json.Unmarshal([]byte(metaJSON), &meta) == nil {
						// Convert to *net.IPNet
						ipNetObj := &net.IPNet{
							IP:   network.IP,
							Mask: network.Mask,
						}
						rc.engine.Insert(ipNetObj, meta)
					}
				}
			}
		}
	}

	return rc
}

// lookupWithCache performs a lookup with caching
func (c *RadixCache) lookupWithCache(ip net.IP) *Metadata {
	ipStr := ip.String()

	// Check cache first (read lock)
	c.cache.RLock()
	if entry, exists := c.entries[ipStr]; exists {
		// Check TTL if configured
		if c.config.TTLSeconds != nil && entry.expiresAt != nil {
			if time.Now().Before(*entry.expiresAt) {
				c.cache.RUnlock()
				return entry.metadata
			}
			// Expired - we'll remove it with write lock
			c.cache.RUnlock()
			c.cache.Lock()
			delete(c.entries, ipStr)
			c.cache.Unlock()
		} else {
			// No TTL, return cached value
			c.cache.RUnlock()
			return entry.metadata
		}
	} else {
		c.cache.RUnlock()
	}

	// Cache miss - query engine
	result := c.engine.Lookup(ip)

	// If not found in engine, check Redis lookup cache
	if result == nil && c.redis != nil {
		ctx := context.Background()
		cachedMeta, err := c.redis.Get(ctx, "radixip:lookup:"+ipStr)
		if err == nil && cachedMeta != "" {
			var meta Metadata
			if json.Unmarshal([]byte(cachedMeta), &meta) == nil {
				result = &meta
			}
		}
	}

	// Store in cache
	c.cache.Lock()
	defer c.cache.Unlock()

	// Check if we need to evict
	if len(c.entries) >= c.config.MaxEntries {
		// Simple eviction - remove oldest (first key)
		for key := range c.entries {
			delete(c.entries, key)
			break
		}
	}

	// Store the result (even if nil)
	var expiresAt *time.Time
	if c.config.TTLSeconds != nil {
		t := time.Now().Add(time.Duration(*c.config.TTLSeconds) * time.Second)
		expiresAt = &t
	}

	c.entries[ipStr] = &cacheEntry{
		metadata:  result,
		expiresAt: expiresAt,
	}

	// Also store in Redis lookup cache
	if c.redis != nil && result != nil {
		ctx := context.Background()
		if jsonData, err := json.Marshal(result); err == nil {
			c.redis.Set(ctx, "radixip:lookup:"+ipStr, string(jsonData))
		}
	}

	return result
}

// invalidate removes cache entries that match the given network prefix
func (c *RadixCache) invalidate(prefix *net.IPNet) {
	c.cache.Lock()
	defer c.cache.Unlock()

	for ipStr := range c.entries {
		ip := net.ParseIP(ipStr)
		if ip != nil && networkContainsIP(prefix, ip) {
			delete(c.entries, ipStr)
		}
	}
}

// clear removes all cache entries
func (c *RadixCache) clear() {
	c.cache.Lock()
	defer c.cache.Unlock()
	c.entries = make(map[string]*cacheEntry)
}

// CachedEngine wraps a RadixEngine with caching functionality
type CachedEngine struct {
	inner RadixEngine
	cache *RadixCache
}

// NewCachedEngine creates a new CachedEngine instance
func NewCachedEngine(inner RadixEngine, config CacheConfig, redisClient *RedisClient) *CachedEngine {
	cache := NewRadixCache(config, inner, redisClient)
	return &CachedEngine{
		inner: inner,
		cache: cache,
	}
}

// Insert adds a prefix with metadata and invalidates cache
func (e *CachedEngine) Insert(prefix *IpNetwork, metadata Metadata) error {
	// Convert to *net.IPNet
	ipNetObj := &net.IPNet{
		IP:   prefix.IP,
		Mask: prefix.Mask,
	}
	if err := e.inner.Insert(ipNetObj, metadata); err != nil {
		return err
	}

	// Persist to Redis
	if e.cache.redis != nil {
		ctx := context.Background()
		if jsonData, err := json.Marshal(metadata); err == nil {
			e.cache.redis.HSet(ctx, "radixip:entries", prefix.String(), string(jsonData))
		}
	}

	// Invalidate relevant cache entries
	netPrefix := net.IPNet{IP: prefix.IP, Mask: prefix.Mask}
	e.cache.invalidate(&netPrefix)
	return nil
}

// Lookup performs a lookup with caching
func (e *CachedEngine) Lookup(ip net.IP) *Metadata {
	return e.cache.lookupWithCache(ip)
}

// Remove deletes a prefix and invalidates cache
func (e *CachedEngine) Remove(prefix *IpNetwork) *Metadata {
	// Convert to *net.IPNet
	ipNetObj := &net.IPNet{
		IP:   prefix.IP,
		Mask: prefix.Mask,
	}
	result := e.inner.Remove(ipNetObj)

	// Remove from Redis
	if e.cache.redis != nil {
		ctx := context.Background()
		e.cache.redis.HDel(ctx, "radixip:entries", prefix.String())
	}

	netPrefix := net.IPNet{IP: prefix.IP, Mask: prefix.Mask}
	e.cache.invalidate(&netPrefix)
	return result
}

// Contains checks if a prefix exists
func (e *CachedEngine) Contains(prefix *IpNetwork) bool {
	// Convert to *net.IPNet
	ipNetObj := &net.IPNet{
		IP:   prefix.IP,
		Mask: prefix.Mask,
	}
	return e.inner.Contains(ipNetObj)
}

// Clear removes all entries and clears cache
func (e *CachedEngine) Clear() {
	e.inner.Clear()
	e.cache.clear()
}

// Size returns the number of entries in the underlying engine
func (e *CachedEngine) Size() int {
	return e.inner.Size()
}

// Stats returns engine statistics
func (e *CachedEngine) Stats() *EngineStats {
	return e.inner.Stats()
}

// Helper function to check if an IP is within a network prefix
func networkContainsIP(prefix *net.IPNet, ip net.IP) bool {
	if prefix.IP.To4() != nil && ip.To4() != nil {
		// IPv4
		return prefix.Contains(ip)
	}
	if prefix.IP.To16() != nil && ip.To16() != nil {
		// IPv6
		return prefix.Contains(ip)
	}
	return false
}
