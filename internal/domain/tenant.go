package domain

import (
	"time"
)

// VoiceTenant represents a tenant in the voice system
type VoiceTenant struct {
	ID           string    `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID     string    `json:"tenant_id" gorm:"type:varchar(255);uniqueIndex:uni_voice_tenants_tenant_id;not null"`
	AstraKey     string    `json:"astra_key" gorm:"type:varchar(255);not null"`
	TenantName   string    `json:"tenant_name" gorm:"type:varchar(255);not null"`
	CustomConfig JSONB     `json:"custom_config" gorm:"type:jsonb"`
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time `json:"updated_at" gorm:"autoUpdateTime"`
	Disabled     bool      `json:"disabled" gorm:"default:false"`
}

// TableName sets the table name for VoiceTenant
func (VoiceTenant) TableName() string {
	return "voice_tenants"
}

// CreateVoiceTenantRequest represents the request to create a new voice tenant
type CreateVoiceTenantRequest struct {
	TenantID     string `json:"tenant_id" validate:"required"`
	AstraKey     string `json:"astra_key" validate:"required"`
	TenantName   string `json:"tenant_name" validate:"required"`
	CustomConfig JSONB  `json:"custom_config,omitempty"`
}

// UpdateVoiceTenantRequest represents the request to update a voice tenant
type UpdateVoiceTenantRequest struct {
	TenantName   *string `json:"tenant_name,omitempty"`
	CustomConfig *JSONB  `json:"custom_config,omitempty"`
	Disabled     *bool   `json:"disabled,omitempty"`
}

// VoiceTenantWithAgents represents a tenant with its associated agents
type VoiceTenantWithAgents struct {
	VoiceTenant
	Agents []VoiceAgent `json:"agents"`
}
