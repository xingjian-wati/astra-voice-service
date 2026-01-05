package webrtc

import (
	"github.com/ClareAI/astra-voice-service/internal/core/event"
	"layeh.com/gopus"
)

// OpusWriter interface for audio output
type OpusWriter interface {
	WriteOpusFrame(opusPayload []byte) error
}

// ConnectionInterface defines the interface for connection operations needed by processor
type ConnectionInterface interface {
	GetWAOutputTrack() OpusWriter
	SetWAOutputTrack(writer OpusWriter)
	NeedsAudioCaching() bool
	IsClosed() bool
	SetOpusDecoder(decoder *gopus.Decoder)
	GetOpusDecoder() *gopus.Decoder
	ShouldForwardAudioToAI() (bool, string)
	GetAIWebRTC() *Client
	UpdateLastActivity()
	GetConversationID() string
	GetAgentID() string
	GetChannelTypeString() string
	GetIsAIReady() bool
}

// ServiceInterface defines the interface for service operations needed by processor
// This breaks the circular dependency between processor and service
type ServiceInterface interface {
	GetConnection(connectionID string) ConnectionInterface
	GetEventBus() event.EventBus
	GetSTUNServers() []string
	GetTURNCredentials() []TURNCredentials
}

// TURNCredentials represents TURN server credentials
type TURNCredentials struct {
	URLs       []string
	Username   string
	Credential string
}
