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

// Publish publishes a message to a channel
func (r *RedisClient) Publish(ctx context.Context, channel string, message string) error {
	err := r.inner.client.Publish(ctx, channel, message).Err()
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}

// PublishJSON publishes a JSON-serialized message to a channel
func (r *RedisClient) PublishJSON(ctx context.Context, channel string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}
	return r.Publish(ctx, channel, string(jsonData))
}

// Subscribe subscribes to a channel and processes messages with a callback
func (r *RedisClient) Subscribe(ctx context.Context, channel string, callback func(PubSubMessage)) error {
	pubsub := r.inner.client.Subscribe(ctx, channel)
	defer pubsub.Close()

	ch := pubsub.Channel()

	r.inner.mu.Lock()
	if r.inner.subscribers[channel] == nil {
		r.inner.subscribers[channel] = make([]chan PubSubMessage, 0)
	}
	//100 messages at a time
	msgCh := make(chan PubSubMessage, 100)
	r.inner.subscribers[channel] = append(r.inner.subscribers[channel], msgCh)
	r.inner.mu.Unlock()

	r.inner.subscriptions.Add(1)
	defer r.inner.subscriptions.Done()

	for {
		select {
		case msg := <-ch:
			pubsubMsg := PubSubMessage{
				Channel: msg.Channel,
				Payload: msg.Payload,
				Pattern: msg.Pattern,
			}
			
			// Send to internal subscribers
			r.inner.mu.RLock()
			for _, ch := range r.inner.subscribers[channel] {
				select {
				case ch <- pubsubMsg:
				default:
					// Channel full, skip
				}
			}
			r.inner.mu.RUnlock()
			
			// Call the callback
			callback(pubsubMsg)
			
		case <-r.inner.shutdownCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// SubscribeToChannel subscribes to a channel and returns a channel of messages
func (r *RedisClient) SubscribeToChannel(ctx context.Context, channel string) (<-chan PubSubMessage, error) {
	pubsub := r.inner.client.Subscribe(ctx, channel)
	ch := pubsub.Channel()

	msgCh := make(chan PubSubMessage, 100)
	
	r.inner.subscriptions.Add(1)
	go func() {
		defer r.inner.subscriptions.Done()
		defer close(msgCh)
		
		for {
			select {
			case msg := <-ch:
				msgCh <- PubSubMessage{
					Channel: msg.Channel,
					Payload: msg.Payload,
					Pattern: msg.Pattern,
				}
			case <-r.inner.shutdownCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return msgCh, nil
}
