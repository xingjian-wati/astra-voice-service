package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// VoiceConversationRepository handles database operations for voice conversations
type VoiceConversationRepository struct {
	db *gorm.DB
}

// NewVoiceConversationRepository creates a new voice conversation repository
func NewVoiceConversationRepository(db *gorm.DB) *VoiceConversationRepository {
	return &VoiceConversationRepository{db: db}
}

// Create creates a new voice conversation
func (r *VoiceConversationRepository) Create(ctx context.Context, conversation *domain.VoiceConversation) error {
	if conversation.ID == "" {
		conversation.ID = uuid.New().String()
	}
	if conversation.CreatedAt.IsZero() {
		conversation.CreatedAt = time.Now()
	}
	conversation.UpdatedAt = time.Now()

	if err := r.db.WithContext(ctx).Create(conversation).Error; err != nil {
		return fmt.Errorf("failed to create voice conversation: %w", err)
	}
	return nil
}

// Update updates an existing voice conversation
func (r *VoiceConversationRepository) Update(ctx context.Context, conversation *domain.VoiceConversation) error {
	conversation.UpdatedAt = time.Now()
	if err := r.db.WithContext(ctx).Save(conversation).Error; err != nil {
		return fmt.Errorf("failed to update voice conversation: %w", err)
	}
	return nil
}

// EndConversation ends a voice conversation
func (r *VoiceConversationRepository) EndConversation(ctx context.Context, conversation *domain.VoiceConversation) error {
	now := time.Now()
	conversation.EndedAt = now
	conversation.UpdatedAt = now
	if err := r.db.WithContext(ctx).Save(conversation).Error; err != nil {
		return fmt.Errorf("failed to end voice conversation: %w", err)
	}
	return nil
}

// GetByExternalConversationID retrieves a voice conversation by external conversation ID (call_id)
func (r *VoiceConversationRepository) GetByExternalConversationID(ctx context.Context, externalConversationID string) (*domain.VoiceConversation, error) {
	var conversation domain.VoiceConversation
	if err := r.db.WithContext(ctx).Where("external_conversation_id = ?", externalConversationID).First(&conversation).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get voice conversation: %w", err)
	}
	return &conversation, nil
}

// CreateByExternalID creates a voice conversation using external conversation ID
func (r *VoiceConversationRepository) CreateByExternalID(ctx context.Context, externalConversationID, voiceAgentID string, startedAt, endedAt time.Time) (*domain.VoiceConversation, error) {
	if externalConversationID == "" {
		return nil, fmt.Errorf("external conversation ID cannot be empty")
	}
	if voiceAgentID == "" {
		return nil, fmt.Errorf("voice agent ID cannot be empty")
	}

	// Check if conversation already exists
	existing, err := r.GetByExternalConversationID(ctx, externalConversationID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Update existing conversation
		existing.EndedAt = endedAt
		if err := r.Update(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}

	// Create new conversation
	now := time.Now()
	conversation := &domain.VoiceConversation{
		ID:                     uuid.New().String(),
		ExternalConversationID: externalConversationID,
		VoiceAgentID:           voiceAgentID,
		StartedAt:              startedAt,
		EndedAt:                endedAt,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	if err := r.Create(ctx, conversation); err != nil {
		return nil, err
	}

	return conversation, nil
}

// FindByVoiceAgentID finds voice conversations for a given voice agent ID with optional filters
func (r *VoiceConversationRepository) FindByVoiceAgentID(ctx context.Context, voiceAgentID string, startTime, endTime time.Time, voiceSource domain.ConversationSource) ([]*domain.VoiceConversation, error) {
	if voiceAgentID == "" {
		return nil, fmt.Errorf("voice agent ID cannot be empty")
	}

	query := r.db.WithContext(ctx).Table("voice_conversations").
		Where("voice_agent_id = ? AND started_at BETWEEN ? AND ?", voiceAgentID, startTime, endTime)
	if voiceSource != "" {
		query = query.Where("source = ?", voiceSource)
	}
	query = query.Order("started_at DESC")

	var conversations []*domain.VoiceConversation
	result := query.Find(&conversations)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to find voice conversations: %w", result.Error)
	}

	return conversations, nil
}

// VoiceMessageRepository handles database operations for voice messages
type VoiceMessageRepository struct {
	db *gorm.DB
}

// NewVoiceMessageRepository creates a new voice message repository
func NewVoiceMessageRepository(db *gorm.DB) *VoiceMessageRepository {
	return &VoiceMessageRepository{db: db}
}

// Create creates a single voice message
func (r *VoiceMessageRepository) Create(ctx context.Context, message *domain.VoiceMessage) error {
	if message.ID == "" {
		message.ID = uuid.New().String()
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now()
	}
	message.UpdatedAt = time.Now()

	if err := r.db.WithContext(ctx).Create(message).Error; err != nil {
		return fmt.Errorf("failed to create voice message: %w", err)
	}
	return nil
}

// CreateBatch creates multiple voice messages in a batch
func (r *VoiceMessageRepository) CreateBatch(ctx context.Context, messages []*domain.VoiceMessage) error {
	if len(messages) == 0 {
		return nil
	}

	now := time.Now()
	for _, msg := range messages {
		if msg.ID == "" {
			msg.ID = uuid.New().String()
		}
		if msg.CreatedAt.IsZero() {
			msg.CreatedAt = now
		}
		msg.UpdatedAt = now
	}

	if err := r.db.WithContext(ctx).CreateInBatches(messages, 100).Error; err != nil {
		return fmt.Errorf("failed to create voice messages: %w", err)
	}
	return nil
}

// Update updates an existing voice message content and confidence
func (r *VoiceMessageRepository) Update(ctx context.Context, id string, content string, confidence float64, originalContent string, originalConfidence float64) error {
	updates := map[string]interface{}{
		"content":             content,
		"confidence":          confidence,
		"original_content":    originalContent,
		"original_confidence": originalConfidence,
		"updated_at":          time.Now(),
	}
	if err := r.db.WithContext(ctx).Model(&domain.VoiceMessage{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update voice message: %w", err)
	}
	return nil
}

// GetByConversationID retrieves all voice messages for a conversation
func (r *VoiceMessageRepository) GetByConversationID(ctx context.Context, conversationID string) ([]*domain.VoiceMessage, error) {
	var messages []*domain.VoiceMessage
	if err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND (role != ? OR confidence >= ? OR confidence IS NULL)", conversationID, config.MessageRoleUser, config.DefaultConfidenceThreshold).
		Order("created_at ASC").
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("failed to get voice messages: %w", err)
	}
	return messages, nil
}

// DeleteByConversationID deletes all voice messages for a conversation
func (r *VoiceMessageRepository) DeleteByConversationID(ctx context.Context, conversationID string) error {
	if err := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Delete(&domain.VoiceMessage{}).Error; err != nil {
		return fmt.Errorf("failed to delete voice messages: %w", err)
	}
	return nil
}

// GetByID retrieves a voice conversation by ID
func (r *VoiceConversationRepository) GetByID(ctx context.Context, id string) (*domain.VoiceConversation, error) {
	var conversation domain.VoiceConversation
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&conversation).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get voice conversation: %w", err)
	}
	return &conversation, nil
}

// Delete deletes a voice conversation by ID
func (r *VoiceConversationRepository) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.VoiceConversation{}).Error; err != nil {
		return fmt.Errorf("failed to delete voice conversation: %w", err)
	}
	return nil
}

// Exists checks if a voice conversation exists by ID
func (r *VoiceConversationRepository) Exists(ctx context.Context, id string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.VoiceConversation{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check voice conversation existence: %w", err)
	}
	return count > 0, nil
}
