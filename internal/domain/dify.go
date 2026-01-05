package domain

import (
	"time"
)

// DifyApiToken represents the api_tokens table in Dify database
type DifyApiToken struct {
	ID         string     `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	AppID      *string    `gorm:"type:uuid;index" json:"app_id,omitempty"`
	Type       string     `gorm:"type:varchar(16);not null" json:"type"`
	Token      string     `gorm:"type:varchar(255);not null;index" json:"token"`
	LastUsedAt *time.Time `gorm:"type:timestamp" json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `gorm:"not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	TenantID   *string    `gorm:"type:uuid;index" json:"tenant_id,omitempty"`
}

// TableName specifies the table name for DifyApiToken model
func (DifyApiToken) TableName() string {
	return "api_tokens"
}
