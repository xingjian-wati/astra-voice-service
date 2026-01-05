package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/pkg/redis"
	"go.uber.org/zap"
)

const (
	CleanupChannel   = "astra:voice:session:cleanup"
	SessionKeyPrefix = "astra:voice:session:info"
	SessionTTL       = 1 * time.Hour
)

// SessionInfo represents monitoring data for a call session
type SessionInfo struct {
	SessionID   string    `json:"sessionId"`
	PodID       string    `json:"podId"`
	AgentID     string    `json:"agentId"`
	StartTime   time.Time `json:"startTime"`
	ChannelType string    `json:"channelType"`
}

// CleanupMessage is the payload for cleanup broadcast
type CleanupMessage struct {
	SessionID string `json:"sessionId"`
}

type Manager struct {
	redisSvc redis.RedisServiceInterface
	podID    string
}

func NewManager(redisSvc redis.RedisServiceInterface, podID string) *Manager {
	return &Manager{
		redisSvc: redisSvc,
		podID:    podID,
	}
}

// Register session for monitoring
func (m *Manager) Register(ctx context.Context, info SessionInfo) error {
	info.PodID = m.podID
	if info.StartTime.IsZero() {
		info.StartTime = time.Now()
	}

	data, _ := json.Marshal(info)
	key := fmt.Sprintf("%s:%s", SessionKeyPrefix, info.SessionID)

	err := m.redisSvc.SetValue(ctx, key, string(data), SessionTTL)
	if err == nil {
		logger.Base().Info("Session registered in Redis", zap.String("session_id", info.SessionID), zap.String("pod_id", m.podID))
	}
	return err
}

// Unregister session from monitoring
func (m *Manager) Unregister(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf("%s:%s", SessionKeyPrefix, sessionID)
	return m.redisSvc.DelValue(ctx, key)
}

// NotifyCleanup broadcasts a cleanup request to all pods
func (m *Manager) NotifyCleanup(ctx context.Context, sessionID string) error {
	logger.Base().Info("Broadcasting cleanup request", zap.String("session_id", sessionID))
	return m.redisSvc.Publish(ctx, CleanupChannel, CleanupMessage{SessionID: sessionID})
}

// SubscribeToCleanup listens for cleanup broadcasts
func (m *Manager) SubscribeToCleanup(ctx context.Context, handler func(sessionID string)) error {
	return m.redisSvc.Subscribe(ctx, CleanupChannel, func(payload string) {
		var msg CleanupMessage
		if err := json.Unmarshal([]byte(payload), &msg); err != nil {
			logger.Base().Error("Failed to unmarshal cleanup message", zap.Error(err))
			return
		}
		handler(msg.SessionID)
	})
}
