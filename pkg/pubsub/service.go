package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/ClareAI/astra-protocol/api/event"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type PubSubConfig struct {
	ProjectID string `mapstructure:"project_id"`
	TopicName string `mapstructure:"topic_name"`
	PubID     string `mapstructure:"pub_id"`
	// ConvMetricsPrefix is used specifically for conversation metrics events
	// to align with subscription filters (e.g., "", "beta", "qa", "stage").
	// If empty, it will fall back to PubID for backward compatibility.
	ConvMetricsPrefix string `mapstructure:"conv_metrics_prefix"`
}

type PubSubService struct {
	client *pubsub.Client
	topic  *pubsub.Topic
	config *PubSubConfig
}

// UsageEventOptions contains options for publishing usage events
type UsageEventOptions struct {
	TenantID   string
	AgentID    string
	EventType  event.UsageEventType
	DeltaCount int32
	SalesType  *event.SalesEvent_SalesType
}

// ConversationMetricsEvent models voice_agent conversation metrics payload for Pub/Sub
type ConversationMetricsEvent struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TenantID  string     `json:"tenant_id"`
	AgentID   string     `json:"agent_id"`
	Channel   string     `json:"channel"`
	Language  string     `json:"language"`
	Status    string     `json:"status"`
	StartAt   time.Time  `json:"start_at"`
	EndAt     *time.Time `json:"end_at,omitempty"`
	Duration  int        `json:"duration"`
	TurnCount int        `json:"turn_count"`
	Messages  []Message  `json:"messages,omitempty"`
	Actions   []Action   `json:"actions,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Message represents a single conversation message timing window
type Message struct {
	ID      string `json:"id"`
	StartAt string `json:"startAt"`
	EndAt   string `json:"endAt"`
}

// Action represents a tool action within a conversation
type Action struct {
	ToolName string `json:"toolName"`
	AtID     string `json:"at_id"`
	Param    string `json:"param"`
	Result   bool   `json:"result"`
}

func NewPubSubService(ctx context.Context, cfg *PubSubConfig) (*PubSubService, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("PubSub project ID is required")
	}

	client, err := pubsub.NewClient(ctx, cfg.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create PubSub client: %w", err)
	}

	topic := client.Topic(cfg.TopicName)
	exists, err := topic.Exists(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to check if topic exists: %w", err)
	}

	if !exists {
		logger.Base().Info("📢 Topic does not exist, creating", zap.String("topicname", cfg.TopicName))
		topic, err = client.CreateTopic(ctx, cfg.TopicName)
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("failed to create topic %s: %w", cfg.TopicName, err)
		}
		logger.Base().Info("Topic created successfully", zap.String("topicname", cfg.TopicName))
	}

	return &PubSubService{
		client: client,
		topic:  topic,
		config: cfg,
	}, nil
}

// PublishUsageEvent publishes a usage event with the specified options
func (p *PubSubService) PublishUsageEvent(ctx context.Context, options UsageEventOptions) error {
	var usageEvent *event.TenantUsageEvent

	switch options.EventType {
	case event.UsageEventType_EVENT_CHAT_MESSAGE:
		usageEvent = &event.TenantUsageEvent{
			TenantId:        options.TenantID,
			EventType:       options.EventType,
			UsageDeltaCount: options.DeltaCount,
			CreatedAt:       timestamppb.Now(),
			UsageEvent: &event.TenantUsageEvent_ChatMessage{
				ChatMessage: &event.ChatMessageEvent{
					AgentId: options.AgentID,
				},
			},
		}

	case event.UsageEventType_EVENT_VOICE_MESSAGE:
		usageEvent = &event.TenantUsageEvent{
			TenantId:        options.TenantID,
			EventType:       options.EventType,
			UsageDeltaCount: options.DeltaCount,
			CreatedAt:       timestamppb.Now(),
			UsageEvent: &event.TenantUsageEvent_VoiceMessage{
				VoiceMessage: &event.VoiceMessageEvent{
					AgentId: options.AgentID,
				},
			},
		}

	case event.UsageEventType_EVENT_SALES:
		if options.SalesType == nil {
			return fmt.Errorf("sales type is required for sales events")
		}
		usageEvent = &event.TenantUsageEvent{
			TenantId:        options.TenantID,
			EventType:       options.EventType,
			UsageDeltaCount: options.DeltaCount,
			CreatedAt:       timestamppb.Now(),
			UsageEvent: &event.TenantUsageEvent_Sales{
				Sales: &event.SalesEvent{
					AgentId:  options.AgentID,
					SaleType: *options.SalesType,
				},
			},
		}

	default:
		return fmt.Errorf("unsupported event type: %v", options.EventType)
	}

	return p.publishEvent(ctx, usageEvent)
}

// Convenience methods for easier usage
func (p *PubSubService) PublishChatMessageUsageEvent(ctx context.Context, tenantID, agentID string) error {
	return p.PublishUsageEvent(ctx, UsageEventOptions{
		TenantID:   tenantID,
		AgentID:    agentID,
		EventType:  event.UsageEventType_EVENT_CHAT_MESSAGE,
		DeltaCount: 1,
	})
}

func (p *PubSubService) PublishVoiceMessageUsageEvent(ctx context.Context, tenantID, agentID string, durationSeconds int32) error {
	// // Calculate credits based on duration: minimum base credits, then additional credits per minute
	// credits := int32(VoiceMessageBaseCredits)
	// if durationSeconds > 60 {
	// 	// For each additional minute (60 seconds), add more credits
	// 	additionalMinutes := (durationSeconds - 1) / 60
	// 	credits += additionalMinutes * VoiceMessageCreditsPerMinute
	// }
	return p.PublishUsageEvent(ctx, UsageEventOptions{
		TenantID:   tenantID,
		AgentID:    agentID,
		EventType:  event.UsageEventType_EVENT_VOICE_MESSAGE,
		DeltaCount: durationSeconds,
	})
}

func (p *PubSubService) PublishSalesUsageEvent(ctx context.Context, tenantID, agentID string, salesType event.SalesEvent_SalesType) error {
	return p.PublishUsageEvent(ctx, UsageEventOptions{
		TenantID:   tenantID,
		AgentID:    agentID,
		EventType:  event.UsageEventType_EVENT_API_CALL,
		DeltaCount: 1,
		SalesType:  &salesType,
	})
}

// PublishConversationMetricsEvent publishes aggregated voice agent conversation metrics to Pub/Sub
func (p *PubSubService) PublishConversationMetricsEvent(ctx context.Context, metrics ConversationMetricsEvent) error {
	data, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("failed to marshal conversation metrics event: %w", err)
	}

	serviceID := uuid.New().String()
	taskID := uuid.New().String()

	// Use configurable prefix to align with subscription filters.
	// Expected patterns: "conversation:metrics", "beta:conversation:metrics", etc.
	prefixSource := strings.TrimSuffix(p.config.ConvMetricsPrefix, ":")
	if prefixSource == "" {
		prefixSource = strings.TrimSuffix(p.config.PubID, ":")
	}

	namePrefix := prefixSource
	if namePrefix != "" {
		namePrefix += ":"
	}

	message := &pubsub.Message{
		Attributes: map[string]string{
			"name": fmt.Sprintf("%s%s", namePrefix, taskID),
		},
		Data: data,
	}

	result := p.topic.Publish(ctx, message)
	if _, err := result.Get(ctx); err != nil {
		logger.Base().Error("Failed to publish conversation metrics: error= tenant_id= agent_id= service_id= task_id=", zap.String("tenant_id", metrics.TenantID), zap.String("agent_id", metrics.AgentID))
		return fmt.Errorf("failed to publish conversation metrics message: %w", err)
	}

	logger.Base().Info("Published conversation metrics", zap.String("id", metrics.ID), zap.String("tenant_id", metrics.TenantID), zap.String("agent_id", metrics.AgentID), zap.String("channel", metrics.Channel), zap.String("service_id", serviceID), zap.String("task_id", taskID))

	return nil
}

func (p *PubSubService) publishEvent(ctx context.Context, usageEvent *event.TenantUsageEvent) error {
	data, err := proto.Marshal(usageEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal usage event: %w", err)
	}

	serviceID := uuid.New().String()
	taskID := uuid.New().String()

	message := &pubsub.Message{
		Attributes: map[string]string{
			"name": fmt.Sprintf("%s:%s", p.config.PubID, taskID),
		},
		Data: data,
	}

	result := p.topic.Publish(ctx, message)
	_, err = result.Get(ctx)
	if err != nil {
		logger.Base().Error("Failed to publish usage event", zap.String("tenant_id", usageEvent.TenantId), zap.Int32("event_type", int32(usageEvent.EventType)), zap.String("service_id", serviceID), zap.String("task_id", taskID), zap.Error(err))
		return fmt.Errorf("failed to publish message: %w", err)
	}

	logger.Base().Info("Successfully published usage event", zap.String("tenant_id", usageEvent.TenantId), zap.Int32("event_type", int32(usageEvent.EventType)), zap.Int32("usage_delta_count", usageEvent.UsageDeltaCount), zap.String("service_id", serviceID), zap.String("task_id", taskID))

	return nil
}

func (p *PubSubService) Close() error {
	if p.topic != nil {
		p.topic.Stop()
	}
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}
