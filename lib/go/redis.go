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

// RedisClient wraps the go-redis client
type RedisClient struct {
	inner *RedisClientInner
}

type RedisClientInner struct {
	client          *redis.Client
	config          RedisConfig
	pubsubSender    chan PubSubMessage
	shutdownCh      chan struct{}
	subscriptions   sync.WaitGroup
	mu              sync.RWMutex
	subscribers     map[string][]chan PubSubMessage
}

// NewRedisClient creates a new Redis client
func NewRedisClient(config RedisConfig) (*RedisClient, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:            config.URL,
		PoolSize:        config.PoolSize,
		DialTimeout:     config.ConnectTimeout,
		MaxRetries:      config.MaxRetries,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), config.ConnectTimeout)
	defer cancel()
	
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	inner := &RedisClientInner{
		client:        rdb,
		config:        config,
		pubsubSender:  make(chan PubSubMessage, 100),
		shutdownCh:    make(chan struct{}),
		subscribers:   make(map[string][]chan PubSubMessage),
	}

	return &RedisClient{inner: inner}, nil
}