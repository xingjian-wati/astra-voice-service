package repository

import (
	"context"
	"fmt"

	"github.com/ClareAI/astra-voice-service/internal/domain"
	"gorm.io/gorm"
)

// DifyApiTokenRepository defines the interface for Dify API token operations
type DifyApiTokenRepository interface {
	// GetAppIDByTenantIDAndToken retrieves app_id by tenant_id and token
	GetAppIDByTenantIDAndToken(ctx context.Context, tenantID string, token string) (*string, error)

	// GetByTenantIDAndToken retrieves the full API token object by tenant_id and token
	GetByTenantIDAndToken(ctx context.Context, tenantID string, token string) (*domain.DifyApiToken, error)
}

// GormDifyApiTokenRepository implements DifyApiTokenRepository using GORM
type GormDifyApiTokenRepository struct {
	db *gorm.DB
}

// NewGormDifyApiTokenRepository creates a new GORM Dify API token repository
func NewGormDifyApiTokenRepository(db *gorm.DB) *GormDifyApiTokenRepository {
	return &GormDifyApiTokenRepository{db: db}
}

// GetAppIDByTenantIDAndToken retrieves the app_id by tenant_id and token
func (r *GormDifyApiTokenRepository) GetAppIDByTenantIDAndToken(ctx context.Context, tenantID string, token string) (*string, error) {
	var apiToken domain.DifyApiToken
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND token = ?", tenantID, token).
		First(&apiToken).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("API token not found for tenant: %s and token: %s", tenantID, token)
		}
		return nil, fmt.Errorf("failed to get app_id by tenant_id and token: %w", err)
	}

	return apiToken.AppID, nil
}

// GetByTenantIDAndToken retrieves the full API token object by tenant_id and token
func (r *GormDifyApiTokenRepository) GetByTenantIDAndToken(ctx context.Context, tenantID string, token string) (*domain.DifyApiToken, error) {
	var apiToken domain.DifyApiToken
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND token = ?", tenantID, token).
		First(&apiToken).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("API token not found for tenant: %s and token: %s", tenantID, token)
		}
		return nil, fmt.Errorf("failed to get API token by tenant_id and token: %w", err)
	}

	return &apiToken, nil
}
