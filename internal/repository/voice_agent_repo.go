package repository

import (
	"context"
	"fmt"

	"github.com/ClareAI/astra-voice-service/internal/domain"
	"gorm.io/gorm"
)

// GormVoiceAgentRepository implements VoiceAgentRepository using GORM
type GormVoiceAgentRepository struct {
	db *gorm.DB
}

// NewGormVoiceAgentRepository creates a new GORM voice agent repository
func NewGormVoiceAgentRepository(db *gorm.DB) *GormVoiceAgentRepository {
	return &GormVoiceAgentRepository{db: db}
}

// Create creates a new voice agent
func (r *GormVoiceAgentRepository) Create(ctx context.Context, req *domain.CreateVoiceAgentRequest) (*domain.VoiceAgent, error) {
	agent := &domain.VoiceAgent{
		VoiceTenantID: req.VoiceTenantID,
		AgentName:     req.AgentName,
		TextAgentID:   req.TextAgentID,
		Instruction:   req.Instruction,
		AgentConfig:   req.AgentConfig,
	}

	if err := r.db.WithContext(ctx).Create(agent).Error; err != nil {
		return nil, fmt.Errorf("failed to create voice agent: %w", err)
	}

	return agent, nil
}

// GetByID retrieves a voice agent by ID
func (r *GormVoiceAgentRepository) GetByID(ctx context.Context, id string) (*domain.VoiceAgent, error) {
	var agent domain.VoiceAgent
	if err := r.db.WithContext(ctx).First(&agent, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("voice agent not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get voice agent: %w", err)
	}

	return &agent, nil
}

// GetByTenantID retrieves voice agents by tenant ID
func (r *GormVoiceAgentRepository) GetByTenantID(ctx context.Context, tenantID string, includeDisabled bool) ([]*domain.VoiceAgent, error) {
	var agents []*domain.VoiceAgent
	query := r.db.WithContext(ctx).Where("voice_tenant_id = ?", tenantID)

	if !includeDisabled {
		query = query.Where("disabled = ?", false)
	}

	if err := query.Order("created_at DESC").Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("failed to get voice agents by tenant ID: %w", err)
	}

	return agents, nil
}

// GetAll retrieves all voice agents
func (r *GormVoiceAgentRepository) GetAll(ctx context.Context, includeDisabled bool) ([]*domain.VoiceAgent, error) {
	var agents []*domain.VoiceAgent
	query := r.db.WithContext(ctx)

	if !includeDisabled {
		query = query.Where("disabled = ?", false)
	}

	if err := query.Order("created_at DESC").Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("failed to get voice agents: %w", err)
	}

	return agents, nil
}

// Update updates a voice agent
func (r *GormVoiceAgentRepository) Update(ctx context.Context, id string, req *domain.UpdateVoiceAgentRequest) (*domain.VoiceAgent, error) {
	var agent domain.VoiceAgent
	if err := r.db.WithContext(ctx).First(&agent, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("voice agent not found: %s", id)
		}
		return nil, fmt.Errorf("failed to find voice agent: %w", err)
	}

	// Build update map
	updates := make(map[string]interface{})

	if req.AgentName != nil {
		updates["agent_name"] = *req.AgentName
	}
	if req.Instruction != nil {
		updates["instruction"] = *req.Instruction
	}
	if req.AgentConfig != nil {
		updates["agent_config"] = *req.AgentConfig
	}
	if req.Disabled != nil {
		updates["disabled"] = *req.Disabled
	}

	if len(updates) == 0 {
		return &agent, nil // No changes
	}

	if err := r.db.WithContext(ctx).Model(&agent).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update voice agent: %w", err)
	}

	return &agent, nil
}

// PublishConfig updates the published configuration of a voice agent
func (r *GormVoiceAgentRepository) PublishConfig(ctx context.Context, id string, config *domain.AgentConfigData) (*domain.VoiceAgent, error) {
	var agent domain.VoiceAgent
	if err := r.db.WithContext(ctx).First(&agent, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("voice agent not found: %s", id)
		}
		return nil, fmt.Errorf("failed to find voice agent: %w", err)
	}

	// Update published config
	if err := r.db.WithContext(ctx).Model(&agent).Update("published_agent_config", config).Error; err != nil {
		return nil, fmt.Errorf("failed to update published config: %w", err)
	}

	// Refresh agent data to return complete object
	// (Alternatively just set the field, but refreshing ensures DB state)
	agent.PublishedAgentConfig = config
	return &agent, nil
}

// Delete soft deletes a voice agent
func (r *GormVoiceAgentRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Model(&domain.VoiceAgent{}).Where("id = ?", id).Update("disabled", true)
	if result.Error != nil {
		return fmt.Errorf("failed to delete voice agent: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("voice agent not found: %s", id)
	}

	return nil
}

// Exists checks if a voice agent exists
func (r *GormVoiceAgentRepository) Exists(ctx context.Context, id string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.VoiceAgent{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check if voice agent exists: %w", err)
	}

	return count > 0, nil
}

// CountByTenantID counts voice agents by tenant ID
func (r *GormVoiceAgentRepository) CountByTenantID(ctx context.Context, tenantID string) (int, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.VoiceAgent{}).Where("voice_tenant_id = ? AND disabled = ?", tenantID, false).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count voice agents by tenant ID: %w", err)
	}

	return int(count), nil
}

// GetByTenantIDAndTextAgentID retrieves a voice agent by tenant ID and text agent ID
func (r *GormVoiceAgentRepository) GetByTenantIDAndTextAgentID(ctx context.Context, tenantID string, textAgentID string) (*domain.VoiceAgent, error) {
	var agent domain.VoiceAgent
	if err := r.db.WithContext(ctx).Where("voice_tenant_id = ? AND text_agent_id = ?", tenantID, textAgentID).First(&agent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("voice agent not found for tenant %s and text agent %s", tenantID, textAgentID)
		}
		return nil, fmt.Errorf("failed to get voice agent: %w", err)
	}

	return &agent, nil
}

// GetByTextAgentID retrieves a voice agent by text agent ID
func (r *GormVoiceAgentRepository) GetByTextAgentID(ctx context.Context, textAgentID string) (*domain.VoiceAgent, error) {
	var agent domain.VoiceAgent
	if err := r.db.WithContext(ctx).Where("text_agent_id = ?", textAgentID).Order("created_at DESC").First(&agent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("voice agent not found for text agent %s", textAgentID)
		}
		return nil, fmt.Errorf("failed to get voice agent: %w", err)
	}

	return &agent, nil
}

// GetAgentAPIKeyByPlatformAgentID retrieves the agent_api_key from voice_agents table
// by platform_agent_id and environment
func (r *GormVoiceAgentRepository) GetAgentAPIKeyByPlatformAgentID(ctx context.Context, platformAgentID string, environment string) (string, error) {
	var platformAgent domain.PlatformVoiceAgent

	query := r.db.WithContext(ctx).
		Where("platform_agent_id = ? AND environment = ?", platformAgentID, environment).
		Where("is_deleted = ?", false)

	if err := query.First(&platformAgent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", fmt.Errorf("agent_api_key not found for platform_agent_id: %s, environment: %s", platformAgentID, environment)
		}
		return "", fmt.Errorf("failed to get agent_api_key: %w", err)
	}

	return platformAgent.AgentAPIKey, nil
}
