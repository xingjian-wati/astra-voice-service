package event

import (
	"time"
)

// EventType represents the type of event
type EventType string

// Connection lifecycle events
const (
	// Connection lifecycle
	ConnectionCreated    EventType = "connection.created"
	ConnectionReady      EventType = "connection.ready"
	ConnectionTerminated EventType = "connection.terminated"

	// WebRTC events
	SDPOfferReceived     EventType = "webrtc.sdp_offer_received"
	SDPAnswerGenerated   EventType = "webrtc.sdp_answer_generated"
	ICEConnectionChanged EventType = "webrtc.ice_connection_changed"
	AudioTrackReady      EventType = "webrtc.audio_track_ready"

	// AI/model events (legacy identifiers kept for compatibility)
	AIConnectionInit   EventType = "ai.connection_initialized"
	AIAudioReady       EventType = "ai.audio_track_ready"
	AIDataChannelReady EventType = "ai.data_channel_ready"
	AIGreetingSent     EventType = "ai.greeting_sent"

	// WhatsApp events
	WhatsAppCallStarted    EventType = "whatsapp.call_started"
	WhatsAppCallAccepted   EventType = "whatsapp.call_accepted"
	WhatsAppCallTerminated EventType = "whatsapp.call_terminated"
	WhatsAppAudioReady     EventType = "whatsapp.audio_ready"

	// Internal/system events
	HandlerPanic EventType = "handler.panic"
)

// ConnectionEvent represents a connection-related event
type ConnectionEvent struct {
	Type         EventType   `json:"type"`
	ConnectionID string      `json:"connection_id"`
	CallID       string      `json:"call_id,omitempty"`
	TenantID     string      `json:"tenant_id,omitempty"`
	Timestamp    time.Time   `json:"timestamp"`
	Data         interface{} `json:"data,omitempty"`
	Error        error       `json:"error,omitempty"`
}

// WebRTCEventData contains WebRTC-specific event data
type WebRTCEventData struct {
	ConnectionID string `json:"connection_id"`
	TrackType    string `json:"track_type,omitempty"`
	SSRC         uint32 `json:"ssrc,omitempty"`
	SDP          string `json:"sdp,omitempty"`
	ICEState     string `json:"ice_state,omitempty"`
	TrackID      string `json:"track_id,omitempty"`
}

// AIEventData contains model-specific event data (was OpenAIEventData)
type AIEventData struct {
	ConnectionID  string `json:"connection_id"`
	IsReady       bool   `json:"is_ready"`
	DataChannelID string `json:"data_channel_id,omitempty"`
	AudioTrackID  string `json:"audio_track_id,omitempty"`
	GreetingSent  bool   `json:"greeting_sent,omitempty"`
	Error         error  `json:"error,omitempty"`
}

// WhatsAppEventData contains WhatsApp-specific event data
type WhatsAppEventData struct {
	ConnectionID   string `json:"connection_id"`
	CallID         string `json:"call_id"`
	PhoneNumber    string `json:"phone_number,omitempty"`
	ContactName    string `json:"contact_name,omitempty"`
	BusinessNumber string `json:"business_number,omitempty"`
	VoiceLanguage  string `json:"voice_language,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
}

// NewConnectionEvent creates a new connection event
func NewConnectionEvent(eventType EventType, connectionID string) *ConnectionEvent {
	return &ConnectionEvent{
		Type:         eventType,
		ConnectionID: connectionID,
		Timestamp:    time.Now(),
	}
}

// WithCallID adds call ID to the event
func (e *ConnectionEvent) WithCallID(callID string) *ConnectionEvent {
	e.CallID = callID
	return e
}

// WithTenantID adds tenant ID to the event
func (e *ConnectionEvent) WithTenantID(tenantID string) *ConnectionEvent {
	e.TenantID = tenantID
	return e
}

// WithData adds data to the event
func (e *ConnectionEvent) WithData(data interface{}) *ConnectionEvent {
	e.Data = data
	return e
}

// WithError adds error to the event
func (e *ConnectionEvent) WithError(err error) *ConnectionEvent {
	e.Error = err
	return e
}

// IsError returns true if the event contains an error
func (e *ConnectionEvent) IsError() bool {
	return e.Error != nil
}

// GetWebRTCData returns WebRTC event data if available
func (e *ConnectionEvent) GetWebRTCData() (*WebRTCEventData, bool) {
	if data, ok := e.Data.(*WebRTCEventData); ok {
		return data, true
	}
	return nil, false
}

// GetAIData returns model event data if available
func (e *ConnectionEvent) GetAIData() (*AIEventData, bool) {
	if data, ok := e.Data.(*AIEventData); ok {
		return data, true
	}
	return nil, false
}

// GetWhatsAppData returns WhatsApp event data if available
func (e *ConnectionEvent) GetWhatsAppData() (*WhatsAppEventData, bool) {
	if data, ok := e.Data.(*WhatsAppEventData); ok {
		return data, true
	}
	return nil, false
}
