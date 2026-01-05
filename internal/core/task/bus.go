package task

import (
	"context"
	"encoding/json"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/pkg/redis"
	"go.uber.org/zap"
)

const (
	TaskChannel = "astra:voice:session:tasks"
)

// RedisBus implements the Bus interface using Redis Pub/Sub
type RedisBus struct {
	redisSvc redis.RedisServiceInterface
}

// NewRedisBus creates a new Redis-based task bus
func NewRedisBus(redisSvc redis.RedisServiceInterface) *RedisBus {
	return &RedisBus{redisSvc: redisSvc}
}

// Publish sends a task to the bus
func (b *RedisBus) Publish(ctx context.Context, task SessionTask) error {
	logger.Base().Debug("Publishing task", zap.String("type", string(task.Type)), zap.String("conn_id", task.ConnectionID))
	return b.redisSvc.Publish(ctx, TaskChannel, task)
}

// Subscribe listens for tasks on the bus
func (b *RedisBus) Subscribe(ctx context.Context, handler func(SessionTask)) error {
	logger.Base().Info("Subscribing to session tasks")
	return b.redisSvc.Subscribe(ctx, TaskChannel, func(payload string) {
		var task SessionTask
		if err := json.Unmarshal([]byte(payload), &task); err != nil {
			logger.Base().Error("Failed to unmarshal task payload", zap.Error(err))
			return
		}
		handler(task)
	})
}
