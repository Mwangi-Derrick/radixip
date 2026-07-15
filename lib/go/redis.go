package go

import ("github.com/redis/go-redis/v9")

// Custom error types
var (
	ErrSendError = errors.New("channel send error")
	ErrRecvError = errors.New("channel receive error")
)

// Configuration for Redis connection
type RedisConfig struct {
	URL            string
	PoolSize       int
	ConnectTimeout time.Duration
	MaxRetries     int
}

func DefaultRedisConfig() RedisConfig {
	return RedisConfig{
		URL:            "redis://127.0.0.1:6379",
		PoolSize:       10,
		ConnectTimeout: 5 * time.Second,
		MaxRetries:     3,
	}
}

// PubSubMessage represents a message from Redis pub/sub
type PubSubMessage struct {
	Channel string
	Payload string
	Pattern *string
}

// RedisCacheUpdate represents cache update operations
type RedisCacheUpdate struct {
	Op       string      `json:"op"`
	Prefix   *IpNetwork  `json:"prefix,omitempty"`
	Metadata *Metadata   `json:"metadata,omitempty"`
}

// Cache update operation constants
const (
	OpInsert = "insert"
	OpRemove = "remove"
	OpClear  = "clear"
)
