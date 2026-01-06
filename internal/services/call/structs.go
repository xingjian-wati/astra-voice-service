package call

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	webrtcadapter "github.com/ClareAI/astra-voice-service/internal/adapters/webrtc"
	"github.com/ClareAI/astra-voice-service/internal/config"
	modelprovider "github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/internal/repository"
	"github.com/ClareAI/astra-voice-service/internal/storage"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/pkg/pubsub"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"layeh.com/gopus"
)

// WhatsApp Call API webhook structures based on https://developers.facebook.com/docs/whatsapp/cloud-api/calling/reference

// ConversationMessage is an alias for config.ConversationMessage
type ConversationMessage = config.ConversationMessage

// WebhookEntry represents the main webhook entry
type WebhookEntry struct {
	Entry  []WebhookChange `json:"entry"`
	Object string          `json:"object"`
}

// WebhookChange represents a change in the webhook
type WebhookChange struct {
	ID      string           `json:"id"`
	Changes []WebhookChanges `json:"changes"`
}

// WebhookChanges represents the actual changes
type WebhookChanges struct {
	Field string           `json:"field"`
	Value WebhookCallValue `json:"value"`
}

// WebhookCallValue represents the call value in webhook
type WebhookCallValue struct {
	Calls            []CallEvent     `json:"calls"`
	Metadata         WebhookMetadata `json:"metadata"`
	MessagingProduct string          `json:"messaging_product"`
}

// CallEvent represents a single call event
type CallEvent struct {
	ID         string          `json:"id"`
	From       string          `json:"from"`
	To         string          `json:"to"`
	Event      string          `json:"event"` // "connect", "disconnect", "ringing", etc.
	Timestamp  string          `json:"timestamp"`
	Direction  string          `json:"direction"` // "BUSINESS_INITIATED" or "USER_INITIATED"
	Session    *SessionData    `json:"session,omitempty"`
	Connection *ConnectionData `json:"connection,omitempty"`
}

// SessionData represents WebRTC session data
type SessionData struct {
	SDPType string `json:"sdp_type"` // "offer", "answer"
	SDP     string `json:"sdp"`      // WebRTC SDP
}

// ConnectionData represents connection information
type ConnectionData struct {
	WebRTC *WebRTCData `json:"webrtc,omitempty"`
}

// WebRTCData represents WebRTC connection data
type WebRTCData struct {
	SDP string `json:"sdp"` // JSON-encoded SDP data
}

// WebhookMetadata represents webhook metadata
type WebhookMetadata struct {
	PhoneNumberID      string `json:"phone_number_id"`
	DisplayPhoneNumber string `json:"display_phone_number"`
}

// WhatsAppCallConnection represents an active WhatsApp call connection
type WhatsAppCallConnection struct {
	ID                  string
	CallID              string
	From                string
	To                  string
	PermissionMessageID string // For tracking permission request
	CreatedAt           time.Time
	LastActivity        time.Time
	IsActive            bool
	AtomicClosed        int32              // Atomic closed state (0=active, 1=closed)
	ChannelType         domain.ChannelType // Channel type (whatsapp, livekit, test)

	// WebRTC related fields
	LocalSDP  string
	RemoteSDP string
	SDPAnswer string

	// Call direction
	IsOutboundCall bool // true for outbound calls (business initiated), false for inbound (user initiated)

	// Model connection (supports multiple providers)
	ModelHandler       modelprovider.ModelHandler    // NEW: Handler used for this connection
	ModelProvider      modelprovider.ProviderType    // NEW: openai or gemini
	ModelConnection    modelprovider.ModelConnection // Generic model connection interface
	AIWebRTC           *webrtcadapter.Client         // Legacy WebRTC client for backward compatibility
	IsAIReady          bool                          // Legacy flag (model ready)
	ResponseInProgress bool
	HasInboundAudio    bool
	PendingAudioBytes  int

	// Conversation history management
	ConversationHistory    []ConversationMessage
	Actions                []pubsub.Action // Tool call logs for metrics
	HasSentGreeting        bool            // Track if initial greeting has been sent
	GreetingSentTime       time.Time       // Time when greeting instruction was sent
	GreetingAudioStartTime time.Time       // Time when greeting audio actually started playing
	HasSwitchedToRealtime  bool            // Track if switched from greeting to realtime mode

	// Language settings
	VoiceLanguage string // Detected voice language (e.g., "en", "zh", "es")
	Accent        string // Detected accent (e.g., "US", "CN", "ES")
	CountryCode   string // Country code from phone number (e.g., "US", "CN", "ES")

	// Contact information
	ContactName string // Contact name from Wati webhook

	// Agent configuration
	AgentID     string // Agent ID for this connection
	TextAgentID string // Text Agent ID for MCP calls

	// Wati webhook information
	TenantID       string // Tenant ID from Wati webhook
	BusinessNumber string // Business Number from Wati webhook

	// Database integration
	ConversationID string                       // Voice conversation ID in database
	RepoManager    repository.RepositoryManager // Repository manager for database operations

	// Audio processing
	PionWriter webrtcadapter.OpusWriter // Current working solution

	// WhatsApp output track for sending model audio to WhatsApp or LiveKit
	// Changed to interface to support both WhatsApp and LiveKit writers
	WAOutputTrack webrtcadapter.OpusWriter // For sending model audio to output

	// Opus decoder for converting WhatsApp audio to PCM16 for model ingestion (per-connection)
	OpusDecoder *gopus.Decoder // Dedicated decoder for this connection to avoid concurrent access issues

	// Sync
	StopKeepalive chan struct{}
	Mutex         sync.RWMutex
}

// ensureVoiceConversation ensures that a VoiceConversation exists for this connection
// Returns the conversation ID and any error
// If startedAt is nil, uses time.Now()
func (c *WhatsAppCallConnection) ensureVoiceConversation(startedAt *time.Time) (string, error) {
	if c.RepoManager == nil {
		return "", fmt.Errorf("repository manager not available")
	}

	if c.CallID == "" {
		return "", fmt.Errorf("call ID is required to initialize voice conversation")
	}

	// Check if conversation ID is already set
	c.Mutex.RLock()
	conversationID := c.ConversationID
	c.Mutex.RUnlock()

	if conversationID != "" {
		return conversationID, nil
	}

	ctx := context.Background()

	// First, try to find existing conversation by ExternalConversationID
	voiceConversation, err := c.RepoManager.VoiceConversation().GetByExternalConversationID(ctx, c.CallID)
	if err != nil {
		return "", fmt.Errorf("failed to query voice conversation by external ID: %w", err)
	}

	// If not found, create a new one
	if voiceConversation == nil {
		startTime := time.Now()
		if startedAt != nil {
			startTime = *startedAt
		}

		// Determine source
		source := domain.ConversationSourceInbound
		if c.ChannelType == domain.ChannelTypeTest || c.ChannelType == domain.ChannelTypeLiveKit {
			source = domain.ConversationSourceTest
		} else if c.IsOutboundCall {
			source = domain.ConversationSourceOutbound
		}

		voiceConversation = &domain.VoiceConversation{
			ExternalConversationID: c.CallID,
			VoiceAgentID:           c.AgentID,
			ContactName:            c.ContactName,
			ContactNumber:          c.From,
			BusinessNumber:         c.BusinessNumber,
			StartedAt:              startTime,
			EndedAt:                startTime, // Will be updated when conversation ends
			Source:                 source,
		}

		if err := c.RepoManager.VoiceConversation().Create(ctx, voiceConversation); err != nil {
			return "", fmt.Errorf("failed to create voice conversation in database: %w", err)
		}
	}

	// Store the conversation ID (either from existing or newly created)
	c.Mutex.Lock()
	c.ConversationID = voiceConversation.ID
	conversationID = voiceConversation.ID
	c.Mutex.Unlock()

	return conversationID, nil
}

// InitializeVoiceConversation initializes or retrieves the VoiceConversation for this connection
// This should be called when the connection is created to start recording the conversation from the beginning
// If not called, AddMessage will create it as a fallback when the first message arrives
func (c *WhatsAppCallConnection) InitializeVoiceConversation() error {
	_, err := c.ensureVoiceConversation(nil)
	return err
}

// AddMessage adds a message to the conversation history and stores it in the database
func (c *WhatsAppCallConnection) AddMessage(role, content string) string {
	return c.AddMessageWithConfidence(role, content, 0)
}

// AddMessageWithConfidence adds a message with confidence score to the conversation history and stores it in the database
func (c *WhatsAppCallConnection) AddMessageWithConfidence(role, content string, confidence float64) string {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	// Default to 100% confidence for non-user roles (assistant, system) if not specified
	if role != config.MessageRoleUser && confidence == 0 {
		confidence = 100.0
	}

	message := ConversationMessage{
		ID:        uuid.New().String(),
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}

	c.ConversationHistory = append(c.ConversationHistory, message)

	// Update last activity time when new message is added
	// This helps track conversation activity for timeout detection
	c.LastActivity = time.Now()

	// Store message in database if repository manager is available
	if c.RepoManager != nil && c.ID != "" && role != config.MessageRoleSystem {
		go func() {
			ctx := context.Background()

			// Ensure voice conversation exists (fallback if InitializeVoiceConversation wasn't called)
			conversationID, err := c.ensureVoiceConversation(&message.Timestamp)
			if err != nil {
				logger.Base().Error("Failed to ensure voice conversation", zap.String("connection_id", c.ID), zap.Error(err))
				return
			}

			// Set conversation ID in audio cache for file naming
			if audioCache := storage.GetAudioCache(); audioCache != nil {
				audioCache.SetConversationID(c.ID, conversationID)
			}

			voiceMessage := &domain.VoiceMessage{
				ID:             message.ID,
				ConversationID: conversationID,
				Role:           role,
				Content:        content,
				Confidence:     confidence,
				CreatedAt:      message.Timestamp,
			}

			if err := c.RepoManager.VoiceMessage().Create(ctx, voiceMessage); err != nil {
				// Log error but don't fail the operation
				logger.Base().Error("Failed to store voice message in database", zap.String("connection_id", c.ID), zap.String("conversation_id", conversationID), zap.Error(err))
			}
		}()
	}
	return message.ID
}

// UpdateMessage updates an existing message in conversation history and database
func (c *WhatsAppCallConnection) UpdateMessage(messageID string, content string, confidence float64, originalContent string, originalConfidence float64) error {
	c.Mutex.Lock()

	// Update in memory history
	found := false
	for i := range c.ConversationHistory {
		if c.ConversationHistory[i].ID == messageID {
			c.ConversationHistory[i].Content = content
			found = true
			break
		}
	}
	c.Mutex.Unlock()

	if !found {
		return fmt.Errorf("message not found in history: %s", messageID)
	}

	// Update in database if repository manager is available
	if c.RepoManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := c.RepoManager.VoiceMessage().Update(ctx, messageID, content, confidence, originalContent, originalConfidence); err != nil {
			logger.Base().Error("Failed to update voice message in database", zap.String("connection_id", c.ID), zap.String("message_id", messageID), zap.Error(err))
			return fmt.Errorf("failed to update voice message in database: %w", err)
		}
	}

	logger.Base().Info("Updated message in conversation history", zap.String("connection_id", c.ID), zap.String("message_id", messageID), zap.String("content", content), zap.Float64("confidence", confidence), zap.String("original_content", originalContent), zap.Float64("original_confidence", originalConfidence))
	return nil
}

// AddAction records a tool action for metrics publishing.
func (c *WhatsAppCallConnection) AddAction(action pubsub.Action) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	c.Actions = append(c.Actions, action)
}

// GetConversationHistory returns a copy of the conversation history in model format
func (c *WhatsAppCallConnection) GetConversationHistory() []modelprovider.ConversationMessage {
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()

	// Return a copy of the conversation history
	// Convert to provider.ConversationMessage format
	history := make([]modelprovider.ConversationMessage, len(c.ConversationHistory))
	for i, msg := range c.ConversationHistory {
		var timestamp interface{} = msg.Timestamp
		history[i] = modelprovider.ConversationMessage{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: timestamp,
		}
	}
	return history
}

// SyncHistoryToAI syncs the conversation history to the model provider
func (c *WhatsAppCallConnection) SyncHistoryToAI() error {
	if c.ModelConnection == nil {
		return fmt.Errorf("model connection not available")
	}

	history := c.GetConversationHistory()
	if len(history) == 0 {
		return nil // No history to sync
	}

	// Convert to provider.ConversationMessage format
	modelMessages := make([]modelprovider.ConversationMessage, len(history))
	for i, msg := range history {
		modelMessages[i] = modelprovider.ConversationMessage{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		}
	}

	return c.ModelConnection.AddConversationHistory(modelMessages)
}

// GetFrom returns the caller's phone number
func (c *WhatsAppCallConnection) GetFrom() string {
	return c.From
}

// GetVoiceLanguage returns the voice language
func (c *WhatsAppCallConnection) GetVoiceLanguage() string {
	return c.VoiceLanguage
}

// GetAccent returns the detected accent
func (c *WhatsAppCallConnection) GetAccent() string {
	return c.Accent
}

// GetContactName returns the contact name
func (c *WhatsAppCallConnection) GetContactName() string {
	return c.ContactName
}

// GetAgentID returns the agent ID for this connection
func (c *WhatsAppCallConnection) GetAgentID() string {
	return c.AgentID
}

// SetAgentID sets the agent ID for this connection
func (c *WhatsAppCallConnection) SetAgentID(agentID string) {
	c.AgentID = agentID
}

// GetTextAgentID returns the text agent ID for MCP calls
func (c *WhatsAppCallConnection) GetTextAgentID() string {
	return c.TextAgentID
}

// SetTextAgentID sets the text agent ID for MCP calls
func (c *WhatsAppCallConnection) SetTextAgentID(textAgentID string) {
	c.TextAgentID = textAgentID
}

// GetTenantID returns the tenant ID from Wati webhook
func (c *WhatsAppCallConnection) GetTenantID() string {
	return c.TenantID
}

// SetTenantID sets the tenant ID from Wati webhook
func (c *WhatsAppCallConnection) SetTenantID(tenantID string) {
	c.TenantID = tenantID
}

// GetBusinessNumber returns the business number from Wati webhook
func (c *WhatsAppCallConnection) GetBusinessNumber() string {
	return c.BusinessNumber
}

// SetBusinessNumber sets the business number from Wati webhook
func (c *WhatsAppCallConnection) SetBusinessNumber(businessNumber string) {
	c.BusinessNumber = businessNumber
}

// GetIsOutbound returns whether this is an outbound call
func (c *WhatsAppCallConnection) GetIsOutbound() bool {
	return c.IsOutboundCall
}

// GetChannelType returns the channel type for this connection
func (c *WhatsAppCallConnection) GetChannelType() domain.ChannelType {
	return c.ChannelType
}

// GetChannelTypeString returns the channel type as a string (for interface compliance)
func (c *WhatsAppCallConnection) GetChannelTypeString() string {
	return string(c.ChannelType)
}

// NeedsAudioCaching returns whether this channel needs audio caching
func (c *WhatsAppCallConnection) NeedsAudioCaching() bool {
	return c.ChannelType != domain.ChannelTypeLiveKit
}

// GetConversationID returns the conversation ID for this connection
func (c *WhatsAppCallConnection) GetConversationID() string {
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()
	return c.ConversationID
}

// GetWAOutputTrack returns the WhatsApp output track
func (c *WhatsAppCallConnection) GetWAOutputTrack() webrtcadapter.OpusWriter {
	if c == nil {
		return nil
	}
	return c.WAOutputTrack
}

// SetWAOutputTrack sets the WhatsApp output track
func (c *WhatsAppCallConnection) SetWAOutputTrack(writer webrtcadapter.OpusWriter) {
	if c == nil {
		return
	}
	c.WAOutputTrack = writer
}

// SetOpusDecoder sets the Opus decoder for this connection
func (c *WhatsAppCallConnection) SetOpusDecoder(decoder *gopus.Decoder) {
	if c == nil {
		return
	}
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	c.OpusDecoder = decoder
}

// GetOpusDecoder returns the Opus decoder for this connection
func (c *WhatsAppCallConnection) GetOpusDecoder() *gopus.Decoder {
	if c == nil {
		return nil
	}
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()
	return c.OpusDecoder
}

// GetAIWebRTC returns the legacy WebRTC client for this connection (backward compatibility)
func (c *WhatsAppCallConnection) GetAIWebRTC() *webrtcadapter.Client {
	if c == nil {
		return nil
	}
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()

	// Try to get from ModelConnection if AIWebRTC is nil
	// This is provider-specific and may not be available for all providers
	type clientGetter interface {
		GetClient() *webrtcadapter.Client
	}
	if c.AIWebRTC == nil && c.ModelConnection != nil {
		if cg, ok := c.ModelConnection.(clientGetter); ok {
			return cg.GetClient()
		}
	}

	return c.AIWebRTC
}

// GetModelConnection returns the model connection (supports multiple providers)
func (c *WhatsAppCallConnection) GetModelConnection() modelprovider.ModelConnection {
	if c == nil {
		return nil
	}
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()
	return c.ModelConnection
}

// UpdateLastActivity updates the last activity time for this connection
func (c *WhatsAppCallConnection) UpdateLastActivity() {
	if c == nil {
		return
	}
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	c.LastActivity = time.Now()
}

// SetAIReady sets the legacy model ready status
func (c *WhatsAppCallConnection) SetAIReady(ready bool) {
	c.IsAIReady = ready
}

// GetIsAIReady returns whether the model is ready (implements ConnectionInterface)
func (c *WhatsAppCallConnection) GetIsAIReady() bool {
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()
	return c.IsAIReady
}

// IsGreetingSent returns whether greeting has been sent
func (c *WhatsAppCallConnection) IsGreetingSent() bool {
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()
	return c.HasSentGreeting
}

// SetGreetingSent sets the greeting sent status
func (c *WhatsAppCallConnection) SetGreetingSent(sent bool) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	c.HasSentGreeting = sent
}

// TryMarkGreetingSent marks greeting as sent only if it was not already sent.
// Returns true if it performed the state change, false if it was already set.
func (c *WhatsAppCallConnection) TryMarkGreetingSent() bool {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	if c.HasSentGreeting {
		return false
	}
	c.HasSentGreeting = true
	c.GreetingSentTime = time.Now()
	return true
}

// SetSwitchedToRealtime sets the switched to realtime status
func (c *WhatsAppCallConnection) SetSwitchedToRealtime(switched bool) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	c.HasSwitchedToRealtime = switched
}

// SetGreetingAudioStartTime sets the time when greeting audio started playing
func (c *WhatsAppCallConnection) SetGreetingAudioStartTime(t time.Time) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	c.GreetingAudioStartTime = t
}

// ShouldForwardAudioToAI determines if user audio should be forwarded to the model
// based on greeting status and connection timing. (Legacy name for compatibility)
// Returns true if audio should be forwarded, false if it should be suppressed.
// Also returns a reason string if suppressed.
func (c *WhatsAppCallConnection) ShouldForwardAudioToAI() (bool, string) {
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()

	// If we've switched to realtime mode (greeting finished), always forward
	if c.HasSwitchedToRealtime {
		return true, ""
	}

	// Greeting phase checks
	if c.GreetingSentTime.IsZero() {
		// Greeting instruction not sent yet - suppress pre-speech
		// Failsafe: if greeting hasn't been sent after 5s from connection creation, allow user to speak
		if time.Since(c.CreatedAt) > 5*time.Second {
			return true, "failsafe_instruction_timeout"
		}
		return false, "greeting_instruction_not_sent"
	}

	if c.GreetingAudioStartTime.IsZero() {
		// Greeting sent but audio not started - suppress thinking time
		// Failsafe: if audio hasn't started after 5s from instruction, allow user to speak
		if time.Since(c.GreetingSentTime) > 5*time.Second {
			return true, "failsafe_audio_start_timeout"
		}
		return false, "greeting_audio_not_started"
	}

	if time.Since(c.GreetingAudioStartTime) < 3*time.Second {
		// Audio started recently - suppress interruption window
		return false, "greeting_interruption_window"
	}

	// Default to forward
	return true, ""
}

// IsClosed returns whether the connection is closed atomically
func (c *WhatsAppCallConnection) IsClosed() bool {
	return atomic.LoadInt32(&c.AtomicClosed) == 1
}

// WasConnected returns whether the call has successfully established a conversation state at any point.
// This returns true if the call was accepted and setup, regardless of whether it is currently open.
func (c *WhatsAppCallConnection) WasConnected() bool {
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()
	// For inbound calls, ConversationID is set immediately.
	// For outbound calls, it's set only after call is accepted.
	// To be safe, we also check if there is any conversation history.
	return c.ConversationID != "" || len(c.ConversationHistory) > 0
}

// WebRTCOffer represents a WebRTC offer
type WebRTCOffer struct {
	Type string `json:"type"` // "offer"
	SDP  string `json:"sdp"`
}

// WebRTCAnswer represents a WebRTC answer
type WebRTCAnswer struct {
	Type string `json:"type"` // "answer"
	SDP  string `json:"sdp"`
}

// ICECandidate represents an ICE candidate
type ICECandidate struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdpMid"`
	SDPMLineIndex int    `json:"sdpMLineIndex"`
}

// CallSettings represents WhatsApp calling settings
type CallSettings struct {
	Status                   string     `json:"status"`                               // "ENABLED" or "DISABLED"
	CallIconVisibility       string     `json:"call_icon_visibility,omitempty"`       // "DEFAULT" or "DISABLE_ALL"
	CallbackPermissionStatus string     `json:"callback_permission_status,omitempty"` // "ENABLED" or "DISABLED"
	CallHours                *CallHours `json:"call_hours,omitempty"`
	SIP                      *SIPConfig `json:"sip,omitempty"`
}

// CallHours represents call hours configuration
type CallHours struct {
	Status               string          `json:"status"` // "ENABLED" or "DISABLED"
	TimezoneID           string          `json:"timezone_id"`
	WeeklyOperatingHours []OperatingHour `json:"weekly_operating_hours"`
	HolidaySchedule      []Holiday       `json:"holiday_schedule,omitempty"`
}

// OperatingHour represents operating hours for a day
type OperatingHour struct {
	DayOfWeek string `json:"day_of_week"` // "MONDAY", "TUESDAY", etc.
	OpenTime  string `json:"open_time"`   // "0400" format
	CloseTime string `json:"close_time"`  // "1020" format
}

// Holiday represents a holiday schedule
type Holiday struct {
	Date      string `json:"date"`       // "2026-01-01" format
	StartTime string `json:"start_time"` // "0000" format
	EndTime   string `json:"end_time"`   // "2359" format
}

// SIPConfig represents SIP configuration
type SIPConfig struct {
	Status  string      `json:"status"` // "ENABLED" or "DISABLED"
	Servers []SIPServer `json:"servers"`
}

// SIPServer represents a SIP server configuration
type SIPServer struct {
	Hostname             string            `json:"hostname"`
	Port                 int               `json:"port"`
	RequestURIUserParams map[string]string `json:"request_uri_user_params,omitempty"`
}
