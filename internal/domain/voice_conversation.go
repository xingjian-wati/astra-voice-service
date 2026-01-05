package domain

import (
	"time"
)

// ConversationSource represents the source of a voice conversation
type ConversationSource string

const (
	ConversationSourceInbound  ConversationSource = "inbound"
	ConversationSourceOutbound ConversationSource = "outbound"
	ConversationSourceTest     ConversationSource = "test"
)

// VoiceConversation represents a voice conversation from Bland AI
type VoiceConversation struct {
	ID                     string             `json:"id" db:"id" gorm:"column:id;primaryKey"`
	ExternalConversationID string             `json:"external_conversation_id" db:"external_conversation_id" gorm:"column:external_conversation_id;unique"`
	VoiceAgentID           string             `json:"voice_agent_id" db:"voice_agent_id" gorm:"column:voice_agent_id"`
	Source                 ConversationSource `json:"source" db:"source" gorm:"column:source"`
	ContactName            string             `json:"contact_name" db:"contact_name" gorm:"column:contact_name"`
	ContactNumber          string             `json:"contact_number" db:"contact_number" gorm:"column:contact_number"`
	BusinessNumber         string             `json:"business_number" db:"business_number" gorm:"column:business_number"`
	StartedAt              time.Time          `json:"started_at" db:"started_at" gorm:"column:started_at"`
	EndedAt                time.Time          `json:"ended_at" db:"ended_at" gorm:"column:ended_at"`
	CreatedAt              time.Time          `json:"created_at" db:"created_at" gorm:"column:created_at"`
	UpdatedAt              time.Time          `json:"updated_at" db:"updated_at" gorm:"column:updated_at"`
}

func (VoiceConversation) TableName() string {
	return "voice_conversations"
}

// VoiceMessage represents a message in a voice conversation
type VoiceMessage struct {
	ID             string    `json:"id" db:"id" gorm:"column:id;primaryKey"`
	ConversationID string    `json:"conversation_id" db:"conversation_id" gorm:"column:conversation_id;index"`
	Role           string    `json:"role" db:"role" gorm:"column:role"` // user, assistant, agent-action
	Content        string    `json:"content" db:"content" gorm:"column:content"`
	OriginalID     int64     `json:"original_id" db:"original_id" gorm:"column:original_id"`
	CreatedAt      time.Time `json:"created_at" db:"created_at" gorm:"column:created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at" gorm:"column:updated_at"`
}

func (VoiceMessage) TableName() string {
	return "voice_messages"
}
