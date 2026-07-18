package radixip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

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
	Op       string     `json:"op"`
	Prefix   *IpNetwork `json:"prefix,omitempty"`
	Metadata *Metadata  `json:"metadata,omitempty"`
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
	client        *redis.Client
	config        RedisConfig
	pubsubSender  chan PubSubMessage
	shutdownCh    chan struct{}
	subscriptions sync.WaitGroup
	mu            sync.RWMutex
	subscribers   map[string][]chan PubSubMessage
}

// NewRedisClient creates a new Redis client
func NewRedisClient(config RedisConfig) (*RedisClient, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         config.URL,
		PoolSize:     config.PoolSize,
		DialTimeout:  config.ConnectTimeout,
		MaxRetries:   config.MaxRetries,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), config.ConnectTimeout)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	inner := &RedisClientInner{
		client:       rdb,
		config:       config,
		pubsubSender: make(chan PubSubMessage, 100),
		shutdownCh:   make(chan struct{}),
		subscribers:  make(map[string][]chan PubSubMessage),
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

// PSubscribe subscribes to a pattern and returns a channel of messages
func (r *RedisClient) PSubscribe(ctx context.Context, pattern string) (<-chan PubSubMessage, error) {
	pubsub := r.inner.client.PSubscribe(ctx, pattern)
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

// Broadcast sends a message to all internal subscribers
func (r *RedisClient) Broadcast(msg PubSubMessage) {
	select {
	case r.inner.pubsubSender <- msg:
	default:
		// Channel full, skip
	}
}

// SubscribeBroadcast returns a channel for broadcast messages
func (r *RedisClient) SubscribeBroadcast() <-chan PubSubMessage {
	return r.inner.pubsubSender
}

// GetConfig returns the Redis configuration
func (r *RedisClient) GetConfig() RedisConfig {
	return r.inner.config
}

// Shutdown shuts down all subscriptions
func (r *RedisClient) Shutdown() {
	close(r.inner.shutdownCh)
	r.inner.subscriptions.Wait()
}

// Set sets a key-value pair
func (r *RedisClient) Set(ctx context.Context, key string, value string) error {
	return r.inner.client.Set(ctx, key, value, 0).Err()
}

// Get gets a value by key
func (r *RedisClient) Get(ctx context.Context, key string) (string, error) {
	return r.inner.client.Get(ctx, key).Result()
}

// HSet sets a field in a hash
func (r *RedisClient) HSet(ctx context.Context, key string, field string, value string) error {
	return r.inner.client.HSet(ctx, key, field, value).Err()
}

// HGetAll gets all fields in a hash
func (r *RedisClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return r.inner.client.HGetAll(ctx, key).Result()
}

// HDel deletes a field in a hash
func (r *RedisClient) HDel(ctx context.Context, key string, field string) error {
	return r.inner.client.HDel(ctx, key, field).Err()
}

// PublishInsert publishes an insert operation
func (r *RedisClient) PublishInsert(ctx context.Context, channel string, prefix IpNetwork, metadata Metadata) error {
	update := RedisCacheUpdate{
		Op:       OpInsert,
		Prefix:   &prefix,
		Metadata: &metadata,
	}
	return r.PublishJSON(ctx, channel, update)
}

// PublishRemove publishes a remove operation
func (r *RedisClient) PublishRemove(ctx context.Context, channel string, prefix IpNetwork) error {
	update := RedisCacheUpdate{
		Op:     OpRemove,
		Prefix: &prefix,
	}
	return r.PublishJSON(ctx, channel, update)
}

// PublishClear publishes a clear operation
func (r *RedisClient) PublishClear(ctx context.Context, channel string) error {
	update := RedisCacheUpdate{
		Op: OpClear,
	}
	return r.PublishJSON(ctx, channel, update)
}

// SubscribeEngineUpdates subscribes to engine updates and applies them
func (r *RedisClient) SubscribeEngineUpdates(ctx context.Context, channel string, engine RadixEngine) error {
	return r.Subscribe(ctx, channel, func(msg PubSubMessage) {
		var update RedisCacheUpdate
		if err := json.Unmarshal([]byte(msg.Payload), &update); err != nil {
			// Log error
			return
		}

		switch update.Op {
		case OpInsert:
			if update.Prefix != nil && update.Metadata != nil {
				if err := engine.Insert(*update.Prefix, *update.Metadata); err != nil {
					// Log error
				}
			}
		case OpRemove:
			if update.Prefix != nil {
				engine.Remove(*update.Prefix)
			}
		case OpClear:
			engine.Clear()
		}
	})
}
