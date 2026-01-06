package provider

import (
	"time"

	webrtcadapter "github.com/ClareAI/astra-voice-service/internal/adapters/webrtc"
	"github.com/ClareAI/astra-voice-service/pkg/pubsub"
)

// CallConnection represents a channel-agnostic call connection interface.
// It can be implemented by WhatsApp, LiveKit, or other channel connections.
type CallConnection interface {
	// Basic connection info
	GetFrom() string
	GetVoiceLanguage() string
	GetAccent() string
	GetContactName() string
	GetAgentID() string
	GetTextAgentID() string
	GetTenantID() string
	GetBusinessNumber() string
	GetIsOutbound() bool
	GetChannelTypeString() string

	// Conversation management

	AddMessage(role, content string) string
	AddMessageWithConfidence(role, content string, confidence float64) string
	UpdateMessage(messageID string, content string, confidence float64, originalContent string, originalConfidence float64) error

	AddAction(action pubsub.Action)
	GetConversationHistory() []ConversationMessage

	// Audio handling
	GetWAOutputTrack() webrtcadapter.OpusWriter
	NeedsAudioCaching() bool

	// State management
	SetAIReady(ready bool)
	IsGreetingSent() bool
	SetGreetingSent(sent bool)
	TryMarkGreetingSent() bool
	IsClosed() bool
	SetSwitchedToRealtime(switched bool)
	SetGreetingAudioStartTime(t time.Time)
}
