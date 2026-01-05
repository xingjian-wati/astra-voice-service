package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	webrtcadapter "github.com/ClareAI/astra-voice-service/internal/adapters/webrtc"
	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

const (
	DefaultOpenAIModel   = "gpt-realtime"
	DefaultOpenAIBaseURL = "https://api.openai.com"
	OpenAIRealtimePath   = "/v1/realtime/calls"
)

// Provider implements ModelProvider for OpenAI Realtime API
type Provider struct {
	config *config.WebSocketConfig
}

// NewProvider creates a new OpenAI provider
func NewProvider(cfg *config.WebSocketConfig) *Provider {
	return &Provider{
		config: cfg,
	}
}

// GetProviderType returns the provider type
func (p *Provider) GetProviderType() provider.ProviderType {
	return provider.ProviderTypeOpenAI
}

// SupportsFeature checks if OpenAI supports a feature
func (p *Provider) SupportsFeature(feature provider.Feature) bool {
	switch feature {
	case provider.FeatureRealtimeAudio, provider.FeatureFunctionCalling, provider.FeatureStreaming, provider.FeatureCustomVoice, provider.FeatureLanguageSwitching:
		return true
	default:
		return false
	}
}

// InitializeConnection initializes a connection to OpenAI
func (p *Provider) InitializeConnection(ctx context.Context, connectionID string, cfg *provider.ConnectionConfig) (provider.ModelConnection, error) {
	// Create WebRTC client
	client := webrtcadapter.NewClient(p.config)
	client.SetSDPExchanger(p.exchangeSDPWithOpenAI)

	// Initialize WebRTC connection with timeout
	initCtx, cancel := context.WithTimeout(ctx, config.DefaultConnectionTimeout)
	defer cancel()

	if err := client.Initialize(initCtx, cfg.Token); err != nil {
		return nil, fmt.Errorf("failed to initialize OpenAI WebRTC client: %w", err)
	}

	return NewConnection(client), nil
}

// exchangeSDPWithOpenAI exchanges SDP with OpenAI Realtime endpoint.
func (p *Provider) exchangeSDPWithOpenAI(ctx context.Context, sdp, token string) (string, error) {
	base := strings.TrimRight(p.config.OpenAIBaseURL, "/")
	if base == "" {
		base = DefaultOpenAIBaseURL
	}
	endpoint := fmt.Sprintf("%s%s?model=%s", base, OpenAIRealtimePath, DefaultOpenAIModel)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(sdp))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/sdp")

	client := &http.Client{Timeout: config.DefaultConnectionTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to exchange SDP: %w", err)
	}
	defer resp.Body.Close()

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("model API returned status %d: %s", resp.StatusCode, string(responseBytes))
	}

	return string(responseBytes), nil
}

// NewConnection creates a new OpenAI connection from an existing client
func NewConnection(client *webrtcadapter.Client) *Connection {
	return &Connection{
		client: client,
	}
}

// Connection wraps webrtcadapter.Client to implement ModelConnection
type Connection struct {
	client *webrtcadapter.Client
}

// SendAudio sends audio data to OpenAI
func (c *Connection) SendAudio(samples []int16) error {
	return c.client.SendAudio(samples)
}

// SendEvent sends an event to OpenAI
func (c *Connection) SendEvent(event map[string]interface{}) error {
	return c.client.SendEvent(event)
}

// AddConversationHistory adds conversation history to OpenAI session
func (c *Connection) AddConversationHistory(messages []provider.ConversationMessage) error {
	// Convert to webrtcadapter.ConversationMessage format
	openaiMessages := make([]webrtcadapter.ConversationMessage, len(messages))
	for i, msg := range messages {
		var timestamp time.Time
		switch t := msg.Timestamp.(type) {
		case time.Time:
			timestamp = t
		case string:
			// Try to parse if it's a string
			if parsed, err := time.Parse(time.RFC3339, t); err == nil {
				timestamp = parsed
			} else {
				timestamp = time.Now()
			}
		default:
			timestamp = time.Now()
		}

		openaiMessages[i] = webrtcadapter.ConversationMessage{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: timestamp,
		}
	}
	return c.client.AddConversationHistory(openaiMessages)
}

// GenerateTTS triggers TTS generation
func (c *Connection) GenerateTTS(text string) error {
	// Create conversation item
	item := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type": "input_text",
					"text": text,
				},
			},
		},
	}

	if err := c.client.SendEvent(item); err != nil {
		return fmt.Errorf("failed to send conversation item: %w", err)
	}

	// Create response
	response := map[string]interface{}{
		"type": "response.create",
	}

	if err := c.client.SendEvent(response); err != nil {
		return fmt.Errorf("failed to create response: %w", err)
	}

	logger.Base().Info("Triggered TTS", zap.String("text", text))
	return nil
}

// Close closes the connection
func (c *Connection) Close() error {
	return c.client.Close()
}

// IsConnected returns whether the connection is active
func (c *Connection) IsConnected() bool {
	return c.client.IsConnected()
}

// GetAudioTrackHandler returns the audio track handler
func (c *Connection) GetAudioTrackHandler() func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	return c.client.AudioTrackHandler
}

// SetAudioTrackHandler sets the audio track handler
func (c *Connection) SetAudioTrackHandler(handler func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)) {
	c.client.AudioTrackHandler = handler
}

// GetEventHandler returns the event handler
func (c *Connection) GetEventHandler() func(event map[string]interface{}) {
	return c.client.EventHandler
}

// SetEventHandler sets the event handler
func (c *Connection) SetEventHandler(handler func(event map[string]interface{})) {
	c.client.EventHandler = handler
}

// GetClient returns the underlying WebRTC client (for backward compatibility)
func (c *Connection) GetClient() *webrtcadapter.Client {
	return c.client
}
