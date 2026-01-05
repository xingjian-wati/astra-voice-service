package repository

import (
	"context"
	"fmt"

	"github.com/ClareAI/astra-voice-service/internal/domain"
	"gorm.io/gorm"
)

// GormVoiceTenantRepository implements VoiceTenantRepository using GORM
type GormVoiceTenantRepository struct {
	db *gorm.DB
}

// NewGormVoiceTenantRepository creates a new GORM voice tenant repository
func NewGormVoiceTenantRepository(db *gorm.DB) *GormVoiceTenantRepository {
	return &GormVoiceTenantRepository{db: db}
}

// Create creates a new voice tenant
func (r *GormVoiceTenantRepository) Create(ctx context.Context, req *domain.CreateVoiceTenantRequest) (*domain.VoiceTenant, error) {
	tenant := &domain.VoiceTenant{
		TenantID:     req.TenantID,
		AstraKey:     req.AstraKey,
		TenantName:   req.TenantName,
		CustomConfig: req.CustomConfig,
	}

	if err := r.db.WithContext(ctx).Create(tenant).Error; err != nil {
		return nil, fmt.Errorf("failed to create voice tenant: %w", err)
	}

	return tenant, nil
}

// GetByID retrieves a voice tenant by ID
func (r *GormVoiceTenantRepository) GetByID(ctx context.Context, id string) (*domain.VoiceTenant, error) {
	var tenant domain.VoiceTenant
	if err := r.db.WithContext(ctx).First(&tenant, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("voice tenant not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get voice tenant: %w", err)
	}

	return &tenant, nil
}

// GetByAstraKey retrieves a voice tenant by Astra key
func (r *GormVoiceTenantRepository) GetByAstraKey(ctx context.Context, astraKey string) (*domain.VoiceTenant, error) {
	var tenant domain.VoiceTenant
	if err := r.db.WithContext(ctx).First(&tenant, "astra_key = ?", astraKey).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("voice tenant not found with astra key: %s", astraKey)
		}
		return nil, fmt.Errorf("failed to get voice tenant by astra key: %w", err)
	}

	return &tenant, nil
}

// GetByTenantID retrieves a voice tenant by tenant ID
func (r *GormVoiceTenantRepository) GetByTenantID(ctx context.Context, tenantID string) (*domain.VoiceTenant, error) {
	var tenant domain.VoiceTenant
	if err := r.db.WithContext(ctx).First(&tenant, "tenant_id = ?", tenantID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("voice tenant not found with tenant ID: %s", tenantID)
		}
		return nil, fmt.Errorf("failed to get voice tenant by tenant ID: %w", err)
	}

	return &tenant, nil
}

// GetAll retrieves all voice tenants
func (r *GormVoiceTenantRepository) GetAll(ctx context.Context, includeDisabled bool) ([]*domain.VoiceTenant, error) {
	var tenants []*domain.VoiceTenant
	query := r.db.WithContext(ctx)

	if !includeDisabled {
		query = query.Where("disabled = ?", false)
	}

	if err := query.Order("created_at DESC").Find(&tenants).Error; err != nil {
		return nil, fmt.Errorf("failed to get voice tenants: %w", err)
	}

	return tenants, nil
}

// GetWithAgents retrieves a voice tenant with its agents
func (r *GormVoiceTenantRepository) GetWithAgents(ctx context.Context, id string) (*domain.VoiceTenantWithAgents, error) {
	var tenant domain.VoiceTenant
	if err := r.db.WithContext(ctx).Preload("Agents", "disabled = ?", false).First(&tenant, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("voice tenant not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get voice tenant with agents: %w", err)
	}

	// Convert to VoiceTenantWithAgents
	result := &domain.VoiceTenantWithAgents{
		VoiceTenant: tenant,
		Agents:      make([]domain.VoiceAgent, 0),
	}

	// Get agents separately to avoid preload issues with soft delete
	// Note: voice_tenant_id now stores the business ID (tenant_id), not UUID
	var agents []domain.VoiceAgent
	if err := r.db.WithContext(ctx).Where("voice_tenant_id = ? AND disabled = ?", tenant.TenantID, false).Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("failed to get agents for tenant: %w", err)
	}

	result.Agents = agents
	return result, nil
}

// Update updates a voice tenant
func (r *GormVoiceTenantRepository) Update(ctx context.Context, id string, req *domain.UpdateVoiceTenantRequest) (*domain.VoiceTenant, error) {
	var tenant domain.VoiceTenant
	if err := r.db.WithContext(ctx).First(&tenant, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("voice tenant not found: %s", id)
		}
		return nil, fmt.Errorf("failed to find voice tenant: %w", err)
	}

	// Build update map
	updates := make(map[string]interface{})

	if req.TenantName != nil {
		updates["tenant_name"] = *req.TenantName
	}
	if req.CustomConfig != nil {
		updates["custom_config"] = *req.CustomConfig
	}
	if req.Disabled != nil {
		updates["disabled"] = *req.Disabled
	}

	if len(updates) == 0 {
		return &tenant, nil // No changes
	}

	if err := r.db.WithContext(ctx).Model(&tenant).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update voice tenant: %w", err)
	}

	return &tenant, nil
}

// Delete soft deletes a voice tenant
func (r *GormVoiceTenantRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Model(&domain.VoiceTenant{}).Where("id = ?", id).Update("disabled", true)
	if result.Error != nil {
		return fmt.Errorf("failed to delete voice tenant: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("voice tenant not found: %s", id)
	}

	return nil
}

// Exists checks if a voice tenant exists
func (r *GormVoiceTenantRepository) Exists(ctx context.Context, id string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.VoiceTenant{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check if voice tenant exists: %w", err)
	}

	return count > 0, nil
}

// ExistsByTenantID checks if a voice tenant exists by tenant ID
func (r *GormVoiceTenantRepository) ExistsByTenantID(ctx context.Context, tenantID string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.VoiceTenant{}).Where("tenant_id = ?", tenantID).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check if voice tenant exists by tenant ID: %w", err)
	}

	return count > 0, nil
}

// ExistsByAstraKey checks if a voice tenant exists by Astra key
func (r *GormVoiceTenantRepository) ExistsByAstraKey(ctx context.Context, astraKey string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.VoiceTenant{}).Where("astra_key = ?", astraKey).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check if voice tenant exists by astra key: %w", err)
	}

	return count > 0, nil
}
