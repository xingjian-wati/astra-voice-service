package gemini

import (
	"context"
	"fmt"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// initializeConnectionInternal initializes a model connection with specific language.
func (h *Handler) initializeConnectionInternal(connectionID, language, accent string) (provider.ModelConnection, error) {
	if h.Provider == nil {
		return nil, fmt.Errorf("model provider not initialized")
	}

	providerName := h.Provider.GetProviderType().String()
	logger.Base().Info("Initializing Gemini model connection",
		zap.String("provider", providerName),
		zap.String("language", language),
		zap.String("connection_id", connectionID))

	initialLanguage := language
	initialAccent := accent

	// Build connection config
	connConfig := &provider.ConnectionConfig{
		Language:    language,
		Accent:      accent,
		Model:       DefaultGeminiModel,
		SessionType: "realtime",
	}

	// Initialize connection using provider
	ctx, cancel := context.WithTimeout(context.Background(), config.DefaultConnectionTimeout)
	defer cancel()

	conn, err := h.Provider.InitializeConnection(ctx, connectionID, connConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize %s connection: %w", providerName, err)
	}

	// Set up audio track handler for receiving model audio
	conn.SetAudioTrackHandler(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		h.HandleModelAudioTrack(connectionID, track, receiver)
	})

	// Set up event handler for Gemini events
	conn.SetEventHandler(func(event map[string]interface{}) {
		h.handleModelEvent(connectionID, event)
	})

	// Store connection
	h.StoreConnection(connectionID, conn)

	// Initialize connection state for timeouts
	var initializedState bool
	var fallbackSilenceConfig *config.SilenceConfig
	if h.AgentConfigGetter != nil && h.ConnectionGetter != nil {
		if callConn := h.ConnectionGetter(connectionID); callConn != nil {
			agentID := callConn.GetAgentID()
			agentConfig, err := h.AgentConfigGetter(context.Background(), agentID, callConn.GetChannelTypeString())
			if err == nil && agentConfig != nil {
				h.InitConnectionState(connectionID, agentConfig.MaxCallDuration, agentConfig.SilenceConfig)
				initializedState = true
			}
			if agentConfig != nil && agentConfig.SilenceConfig != nil {
				fallbackSilenceConfig = agentConfig.SilenceConfig
			}
			if agentConfig != nil {
				if initialLanguage == "" && agentConfig.Language != "" {
					initialLanguage = agentConfig.Language
				}
				if initialAccent == "" && agentConfig.DefaultAccent != "" {
					initialAccent = agentConfig.DefaultAccent
				}
			}
		}
	}

	if !initializedState {
		logger.Base().Warn("Falling back to default connection state for", zap.String("connection_id", connectionID))
		h.InitConnectionState(connectionID, 0, fallbackSilenceConfig)
	}

	// Cache current language/accent
	if initialLanguage != "" || initialAccent != "" {
		h.SetCurrentLanguageAccent(connectionID, initialLanguage, initialAccent)
	}

	logger.Base().Info("Gemini model connection established",
		zap.String("provider", providerName),
		zap.String("connection_id", connectionID))
	return conn, nil
}
