package domain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONB represents a PostgreSQL JSONB field
type JSONB map[string]interface{}

// Value implements the driver.Valuer interface for JSONB
func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements the sql.Scanner interface for JSONB
func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("cannot scan %T into JSONB", value)
	}

	return json.Unmarshal(bytes, j)
}

// CallStatus constants for call session status
const (
	CallStatusActive    = "active"
	CallStatusEnded     = "ended"
	CallStatusFailed    = "failed"
	CallStatusCancelled = "cancelled"
)

// ChannelType represents the type of channel for a connection
type ChannelType string

const (
	ChannelTypeWhatsApp ChannelType = "whatsapp" // WhatsApp channel (needs audio caching)
	ChannelTypeLiveKit  ChannelType = "livekit"  // LiveKit channel (no audio caching)
	ChannelTypeTest     ChannelType = "test"     // Test channel
	ChannelTypeWeb      ChannelType = "web"      // Web channel
)
