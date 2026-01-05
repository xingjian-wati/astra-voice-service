package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

type KeyType string

const (
	USAGE_CONFIG         KeyType = "astra_tenant_usage_config"
	PREVIEW_CONVERSATION KeyType = "astra_preview_conversation"
)

type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

var ErrKeyNotExist = redis.Nil

type RedisServiceInterface interface {
	GenerateKey(keyType KeyType, identifier string) string
	GetValue(ctx context.Context, key string) (string, error)
	SetValue(ctx context.Context, key string, value string, ttl time.Duration) error
	DelValue(ctx context.Context, key string) error
	Publish(ctx context.Context, channel string, message interface{}) error
	Subscribe(ctx context.Context, channel string, handler func(string)) error
}

type RedisService struct {
	client *redis.Client
}

func NewRedisService(config *RedisConfig) (*RedisService, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", config.Host, config.Port),
		Password: config.Password,
		DB:       config.DB,
	})

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisService{
		client: client,
	}, nil
}

// GenerateKey generates a Redis key with the given key type and identifier
func (r *RedisService) GenerateKey(keyType KeyType, identifier string) string {
	return fmt.Sprintf("%s:%s:", string(keyType), identifier)
}

// GetValue gets a value from Redis by key
func (r *RedisService) GetValue(ctx context.Context, key string) (string, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetValue sets a value in Redis with TTL
func (r *RedisService) SetValue(ctx context.Context, key string, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

// DelValue deletes a value from Redis by key
func (r *RedisService) DelValue(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// Publish publishes a message to a Redis channel
func (r *RedisService) Publish(ctx context.Context, channel string, message interface{}) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return r.client.Publish(ctx, channel, data).Err()
}

// Subscribe subscribes to a Redis channel and handles incoming messages
func (r *RedisService) Subscribe(ctx context.Context, channel string, handler func(string)) error {
	pubsub := r.client.Subscribe(ctx, channel)

	go func() {
		defer pubsub.Close()
		ch := pubsub.Channel()
		for msg := range ch {
			handler(msg.Payload)
		}
	}()

	return nil
}

// PreviewMessage represents a single message in preview conversation history
type PreviewMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"` // Message content
	Name    string `json:"name,omitempty"`
}

// GetPreviewHistory retrieves preview conversation history from Redis
func (r *RedisService) GetPreviewHistory(ctx context.Context, conversationID string) ([]PreviewMessage, error) {
	if r.client == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}

	key := r.GenerateKey(PREVIEW_CONVERSATION, conversationID)

	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			// Key doesn't exist, return empty history
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get preview history: %w", err)
	}

	var messages []PreviewMessage
	if err := json.Unmarshal([]byte(val), &messages); err != nil {
		return nil, fmt.Errorf("failed to unmarshal preview history: %w", err)
	}

	return messages, nil
}

// AppendPreviewHistory appends new messages to preview conversation history
func (r *RedisService) AppendPreviewHistory(ctx context.Context, conversationID string, newMessages []PreviewMessage, ttl time.Duration) error {
	if r.client == nil {
		return fmt.Errorf("redis client not initialized")
	}

	key := r.GenerateKey(PREVIEW_CONVERSATION, conversationID)

	// Get existing history
	existingHistory, err := r.GetPreviewHistory(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("failed to get existing history: %w", err)
	}

	// Append new messages
	allMessages := append(existingHistory, newMessages...)

	// Serialize to JSON
	data, err := json.Marshal(allMessages)
	if err != nil {
		return fmt.Errorf("failed to marshal preview history: %w", err)
	}

	// Store with TTL
	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set preview history: %w", err)
	}

	return nil
}

// ClearPreviewHistory removes preview conversation history from Redis
func (r *RedisService) ClearPreviewHistory(ctx context.Context, conversationID string) error {
	if r.client == nil {
		return fmt.Errorf("redis client not initialized")
	}

	key := r.GenerateKey(PREVIEW_CONVERSATION, conversationID)
	return r.client.Del(ctx, key).Err()
}
