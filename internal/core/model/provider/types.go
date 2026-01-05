package provider

import (
	"context"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/pion/webrtc/v3"
)

// ProviderType represents the type of AI model provider
type ProviderType string

const (
	ProviderTypeOpenAI ProviderType = "openai"
	ProviderTypeGemini ProviderType = "gemini"
)

// String returns the string representation of ProviderType
func (pt ProviderType) String() string {
	return string(pt)
}

// IsValid checks if the provider type is valid
func (pt ProviderType) IsValid() bool {
	return pt == ProviderTypeOpenAI || pt == ProviderTypeGemini
}

// ModelProvider defines the interface for different AI model providers (OpenAI, Gemini, etc.)
type ModelProvider interface {
	// InitializeConnection initializes a connection to the model provider
	InitializeConnection(ctx context.Context, connectionID string, config *ConnectionConfig) (ModelConnection, error)

	// GetProviderType returns the type of the provider
	GetProviderType() ProviderType

	// SupportsFeature checks if the provider supports a specific feature
	SupportsFeature(feature Feature) bool
}

// ModelConnection represents an active connection to a model provider
type ModelConnection interface {
	// SendAudio sends audio data to the model
	SendAudio(samples []int16) error

	// SendEvent sends an event to the model
	SendEvent(event map[string]interface{}) error

	// AddConversationHistory adds conversation history to the session
	AddConversationHistory(messages []ConversationMessage) error

	// GenerateTTS triggers text-to-speech generation
	GenerateTTS(text string) error

	// Close closes the connection
	Close() error

	// IsConnected returns whether the connection is active
	IsConnected() bool

	// GetAudioTrackHandler returns the handler for incoming audio tracks
	GetAudioTrackHandler() func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)

	// SetAudioTrackHandler sets the handler for incoming audio tracks
	SetAudioTrackHandler(handler func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver))

	// GetEventHandler returns the handler for events
	GetEventHandler() func(event map[string]interface{})

	// SetEventHandler sets the handler for events
	SetEventHandler(handler func(event map[string]interface{}))
}

// ConnectionConfig contains configuration for initializing a connection
type ConnectionConfig struct {
	Token       string
	Language    string
	Accent      string
	Voice       string
	Speed       float64
	Tools       []interface{}
	Model       string
	SessionType string
}

// Feature represents a capability that a provider may or may not support
type Feature string

const (
	FeatureRealtimeAudio     Feature = "realtime_audio"
	FeatureFunctionCalling   Feature = "function_calling"
	FeatureStreaming         Feature = "streaming"
	FeatureCustomVoice       Feature = "custom_voice"
	FeatureLanguageSwitching Feature = "language_switching"
)

// ConversationMessage represents a message in conversation history
type ConversationMessage struct {
	Role      string      `json:"role"`      // "user", "assistant", "system"
	Content   string      `json:"content"`   // The message content
	Timestamp interface{} `json:"timestamp"` // When the message was created (can be time.Time or string)
}

// ProviderFactory creates model providers based on configuration
type ProviderFactory interface {
	CreateProvider(providerType ProviderType, config *config.WebSocketConfig) (ModelProvider, error)
	CreateHandler(providerType ProviderType, config *config.WebSocketConfig) (ModelHandler, error)
	GetSupportedProviders() []ProviderType
}
