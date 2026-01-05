package gemini

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	webrtcadapter "github.com/ClareAI/astra-voice-service/internal/adapters/webrtc"
	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

const (
	DefaultGeminiModel    = "models/gemini-3-flash"
	DefaultGeminiBaseURL  = "https://generativelanguage.googleapis.com"
	GeminiDataChannelName = "dc0"
	GeminiAPIVersion      = "v1beta"
)

// Provider implements ModelProvider for Google Gemini API
type Provider struct {
	config *config.WebSocketConfig
}

// NewProvider creates a new Gemini provider
func NewProvider(cfg *config.WebSocketConfig) *Provider {
	return &Provider{
		config: cfg,
	}
}

// GetProviderType returns the provider type
func (p *Provider) GetProviderType() provider.ProviderType {
	return provider.ProviderTypeGemini
}

// SupportsFeature checks if Gemini supports a feature
func (p *Provider) SupportsFeature(feature provider.Feature) bool {
	switch feature {
	case provider.FeatureRealtimeAudio, provider.FeatureFunctionCalling, provider.FeatureStreaming:
		return true
	case provider.FeatureCustomVoice, provider.FeatureLanguageSwitching:
		return false
	default:
		return false
	}
}

// InitializeConnection initializes a connection to Gemini
func (p *Provider) InitializeConnection(ctx context.Context, connectionID string, cfg *provider.ConnectionConfig) (provider.ModelConnection, error) {
	// Create WebRTC client
	client := webrtcadapter.NewClient(p.config)
	client.SetSDPExchanger(p.exchangeSDPWithGemini)
	client.SetDataChannelName(GeminiDataChannelName)

	// Initialize WebRTC connection with timeout
	initCtx, cancel := context.WithTimeout(ctx, config.DefaultConnectionTimeout)
	defer cancel()

	// Use model from ConnectionConfig or config default
	modelName := cfg.Model
	if modelName == "" {
		modelName = p.config.GeminiModel
	}
	if modelName == "" {
		modelName = DefaultGeminiModel
	}

	// Ensure models/ prefix if not present
	if !strings.HasPrefix(modelName, "models/") {
		modelName = "models/" + modelName
	}

	// Pass model to exchanger via context
	initCtx = context.WithValue(initCtx, "model", modelName)

	// Use Gemini API Key from config if not provided in ConnectionConfig
	apiKey := cfg.Token
	if apiKey == "" {
		apiKey = p.config.GeminiAPIKey
	}

	if err := client.Initialize(initCtx, apiKey); err != nil {
		return nil, fmt.Errorf("failed to initialize Gemini WebRTC client: %w", err)
	}

	conn := NewConnection(client)

	// Set up Gemini session configuration (setup event)
	client.OnDataChannelOpen = func() {
		setupEvent := map[string]interface{}{
			"setup": map[string]interface{}{
				"model": modelName,
				"generationConfig": map[string]interface{}{
					"responseModalities": []string{"audio"},
				},
			},
		}

		if err := client.SendEvent(setupEvent); err != nil {
			logger.Base().Error("Failed to send Gemini setup event", zap.Error(err))
		} else {
			logger.Base().Info("Sent Gemini setup event", zap.String("model", modelName))
		}
	}

	return conn, nil
}

// exchangeSDPWithGemini exchanges SDP with Gemini Multimodal Live endpoint.
func (p *Provider) exchangeSDPWithGemini(ctx context.Context, sdp, apiKey string) (string, error) {
	base := strings.TrimRight(p.config.GeminiBaseURL, "/")
	if base == "" {
		base = DefaultGeminiBaseURL
	}

	// Use model from context if available
	model := DefaultGeminiModel
	if m, ok := ctx.Value("model").(string); ok && m != "" {
		model = m
	}

	// API endpoint for WebRTC SDP exchange
	endpoint := fmt.Sprintf("%s/%s/%s:bidiGenerateContent?key=%s", base, GeminiAPIVersion, model, apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(sdp))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
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
		return "", fmt.Errorf("gemini API returned status %d: %s", resp.StatusCode, string(responseBytes))
	}

	return string(responseBytes), nil
}

// NewConnection creates a new Gemini connection from an existing client
func NewConnection(client *webrtcadapter.Client) *Connection {
	return &Connection{
		client: client,
	}
}

// Connection wraps webrtcadapter.Client to implement ModelConnection
type Connection struct {
	client *webrtcadapter.Client
}

// SendAudio sends audio data to Gemini
func (c *Connection) SendAudio(samples []int16) error {
	return c.client.SendAudio(samples)
}

// SendEvent sends an event to Gemini
func (c *Connection) SendEvent(event map[string]interface{}) error {
	return c.client.SendEvent(event)
}

// AddConversationHistory adds conversation history to Gemini session
func (c *Connection) AddConversationHistory(messages []provider.ConversationMessage) error {
	// Gemini uses clientContent events for history after setup
	for _, msg := range messages {
		event := map[string]interface{}{
			"clientContent": map[string]interface{}{
				"turns": []map[string]interface{}{
					{
						"role": msg.Role,
						"parts": []map[string]interface{}{
							{
								"text": msg.Content,
							},
						},
					},
				},
				"turnComplete": true,
			},
		}
		if err := c.client.SendEvent(event); err != nil {
			return fmt.Errorf("failed to send history event: %w", err)
		}
	}
	logger.Base().Info("Added conversation history items to Gemini", zap.Int("count", len(messages)))
	return nil
}

// GenerateTTS triggers TTS generation
func (c *Connection) GenerateTTS(text string) error {
	// For Gemini, we send a clientContent event with the text
	event := map[string]interface{}{
		"clientContent": map[string]interface{}{
			"turns": []map[string]interface{}{
				{
					"role": "user",
					"parts": []map[string]interface{}{
						{
							"text": text,
						},
					},
				},
			},
			"turnComplete": true,
		},
	}

	if err := c.client.SendEvent(event); err != nil {
		return fmt.Errorf("failed to send Gemini TTS event: %w", err)
	}

	logger.Base().Info("Triggered Gemini TTS", zap.String("text", text))
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
