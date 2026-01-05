package openai

import (
	"context"
	"fmt"

	webrtcadapter "github.com/ClareAI/astra-voice-service/internal/adapters/webrtc"
	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	toolspkg "github.com/ClareAI/astra-voice-service/internal/core/tool"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// InitializeConnection initializes a model connection (backward compatibility - returns OpenAI client)
func (h *Handler) InitializeConnection(connectionID string) (*webrtcadapter.Client, error) {
	conn, err := h.InitializeConnectionWithLanguage(connectionID, "", "")
	if err != nil {
		return nil, err
	}
	// Return OpenAI client for backward compatibility
	if openaiConn, ok := conn.(*Connection); ok {
		return openaiConn.GetClient(), nil
	}
	return nil, fmt.Errorf("connection is not an OpenAI connection")
}

// InitializeConnectionWithLanguage initializes a model connection with specific language
// Returns ModelConnection interface (supports multiple providers)
func (h *Handler) InitializeConnectionWithLanguage(connectionID, language, accent string) (provider.ModelConnection, error) {
	if h.Provider == nil {
		return nil, fmt.Errorf("model provider not initialized")
	}

	providerName := h.Provider.GetProviderType().String()
	logger.Base().Info("Initializing model connection",
		zap.String("provider", providerName),
		zap.String("language", language),
		zap.String("connection_id", connectionID))

	initialLanguage := language
	initialAccent := accent

	// Generate ephemeral token with agent config support
	var token string
	var err error

	// Get agent config if available
	var voice string
	var speed float64
	if h.ConnectionGetter != nil {
		if conn := h.ConnectionGetter(connectionID); conn != nil {
			agentID := conn.GetAgentID()
			if agentID != "" && h.PromptGenerator != nil {
				if promptGen := h.PromptGenerator(connectionID); promptGen != nil {
					// Try to get agent config from prompt generator
					// (PromptGenerator has access to agent config)
					logger.Base().Info("Attempting to use agent configuration for voice and speed")
					voice, speed = h.getAgentVoiceAndSpeed(agentID, conn.GetChannelTypeString())
				}
			}
		}
	}

	// Get tools for this connection (agent-specific or filtered by AllowedActions)
	tools := h.getToolsForConnection(connectionID)

	// Generate token with voice and speed (fallback to language-based if not configured)
	if voice != "" && speed > 0 {
		logger.Base().Info("Using agent-configured voice", zap.String("voice", voice), zap.Float64("speed", speed))
		token, err = h.TokenGenerator("realtime", DefaultOpenAIModel, voice, language, speed, tools)
	} else if language != "" {
		logger.Base().Info("🔑 Using language-based voice selection for", zap.String("language", language))
		token, err = h.generateEphemeralTokenWithLanguage(language, tools)
	} else {
		logger.Base().Info("🔑 Using default voice configuration")
		token, err = h.generateEphemeralToken(tools)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to generate ephemeral token: %w", err)
	}

	// Build connection config
	connConfig := &provider.ConnectionConfig{
		Token:       token,
		Language:    language,
		Accent:      accent,
		Voice:       voice,
		Speed:       speed,
		Tools:       tools,
		Model:       DefaultOpenAIModel, // Default model, can be made configurable
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

	// Set up event handler for conversation history tracking
	conn.SetEventHandler(func(event map[string]interface{}) {
		h.handleModelEvent(connectionID, event)
	})

	// Store connection
	h.StoreConnection(connectionID, conn)

	// Initialize connection state for timeouts (always set up a safety timer)
	var initializedState bool
	var fallbackSilenceConfig *config.SilenceConfig
	if h.AgentConfigGetter != nil && h.ConnectionGetter != nil {
		if conn := h.ConnectionGetter(connectionID); conn != nil {
			agentID := conn.GetAgentID()
			agentConfig, err := h.AgentConfigGetter(context.Background(), agentID, conn.GetChannelTypeString())
			if err == nil && agentConfig != nil {
				h.InitConnectionState(connectionID, agentConfig.MaxCallDuration, agentConfig.SilenceConfig)
				initializedState = true
			}
			if agentConfig != nil && agentConfig.SilenceConfig != nil {
				fallbackSilenceConfig = agentConfig.SilenceConfig
			}
			// Initialize current language/accent from agent config if available
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

	// Cache current language/accent for later use
	if initialLanguage != "" || initialAccent != "" {
		h.SetCurrentLanguageAccent(connectionID, initialLanguage, initialAccent)
	}

	logger.Base().Info("Model connection established",
		zap.String("provider", providerName),
		zap.String("connection_id", connectionID))
	return conn, nil
}

// generateEphemeralToken generates an ephemeral token for WebRTC connection
func (h *Handler) generateEphemeralToken(tools []interface{}) (string, error) {
	logger.Base().Info("Generating ephemeral token for OpenAI WebRTC", zap.Int("tools_count", len(tools)))

	if h.TokenGenerator == nil {
		return "", fmt.Errorf("token generator not set")
	}

	return h.TokenGenerator("realtime", DefaultOpenAIModel, "verse", "", 1.1, tools)
}

// generateEphemeralTokenWithLanguage generates a language-specific ephemeral token
func (h *Handler) generateEphemeralTokenWithLanguage(language string, tools []interface{}) (string, error) {
	logger.Base().Info("Generating ephemeral token for language", zap.String("language", language), zap.Int("tools_count", len(tools)))

	if h.TokenGenerator == nil {
		return "", fmt.Errorf("token generator not set")
	}

	// Get language-specific configuration
	voice := h.selectVoiceForLanguage(language)
	speed := h.getSpeedForLanguage(language)

	return h.TokenGenerator("realtime", DefaultOpenAIModel, voice, language, speed, tools)
}

// getToolsForConnection returns tools for a specific connection based on agent config
// Uses whitelist approach: only returns tools if explicitly configured
// Automatically adds language switch notification tool when auto language switching is enabled
func (h *Handler) getToolsForConnection(connectionID string) []interface{} {
	var tools []interface{}

	// 1. Check dependencies
	if h.ConnectionGetter == nil || h.AgentConfigGetter == nil {
		return h.appendSystemTools(tools, nil)
	}

	// 2. Get Connection
	conn := h.ConnectionGetter(connectionID)
	if conn == nil {
		return h.appendSystemTools(tools, nil)
	}

	// 3. Get Agent ID
	agentID := conn.GetAgentID()
	if agentID == "" {
		return h.appendSystemTools(tools, nil)
	}

	// 4. Get Agent Config
	agentConfig, err := h.AgentConfigGetter(context.Background(), agentID, conn.GetChannelTypeString())
	if err != nil {
		logger.Base().Error("Failed to get agent config")
	}

	// Use TextAgentID for MCP tools if available
	mcpAgentID := agentID
	if agentConfig != nil && agentConfig.TextAgentID != "" {
		mcpAgentID = agentConfig.TextAgentID
		logger.Base().Info("Using TextAgentID for fetching MCP tools: (VoiceAgentID: )", zap.String("mcpagentid", mcpAgentID), zap.String("agent_id", agentID))
	} else if textAgentID := conn.GetTextAgentID(); textAgentID != "" {
		// Fallback to connection's TextAgentID if config lookup failed but connection has it
		mcpAgentID = textAgentID
		logger.Base().Info("Using TextAgentID from connection for fetching MCP tools", zap.String("mcpagentid", mcpAgentID))
	}

	if mcpTools := h.fetchAndFilterMCPTools(context.Background(), mcpAgentID, conn); len(mcpTools) > 0 {
		tools = append(tools, mcpTools...)
	}
	// Auto-add system notification tools if enabled (regardless of other tools)
	tools = h.appendSystemTools(tools, agentConfig)

	// No configuration found - return empty tools (whitelist approach)
	if len(tools) == 0 {
		logger.Base().Warn("No tool configuration found, returning empty tools (whitelist mode)")
	}
	for idx, tool := range tools {
		if toolMap, ok := tool.(map[string]interface{}); ok {
			if name, exists := toolMap["name"].(string); exists {
				logger.Base().Info("Registered Tool", zap.String("name", name), zap.Int("idx", idx))
			}
		}
	}

	return tools
}

// fetchAndFilterMCPTools fetches tools from MCP service and filters them based on allowed actions
func (h *Handler) fetchAndFilterMCPTools(ctx context.Context, agentID string, conn provider.CallConnection) []interface{} {
	if h.ToolManager == nil {
		return nil
	}

	// Determine mode based on channel type
	mode := config.AgentConfigModePublished
	if conn.GetChannelTypeString() == string(domain.ChannelTypeTest) {
		mode = config.AgentConfigModeDraft
	}

	// Determine modality based on connection direction
	modality := mcp.ModalityVoiceInbound
	if conn.GetIsOutbound() {
		modality = mcp.ModalityVoiceOutbound
	}

	// Fetch tools using clean AgentID (no suffix) and determined mode
	mcpTools, err := h.ToolManager.GetMcpToolDefinitions(ctx, agentID, mode, modality)
	if err != nil {
		// If fetching tools fails, we proceed with empty tools (allowable failure)
		logger.Base().Error("Failed to fetch tools from MCP service")
		return nil
	}

	logger.Base().Info("Added tools from MCP service", zap.String("agent_id", agentID), zap.String("mode", mode), zap.Bool("outbound", conn.GetIsOutbound()))

	return mcpTools
}

// ==========================================
// Tool Configuration Helpers
// ==========================================
// These methods help build the tool list for a connection based on agent configuration

// getDefaultToolsWithFilter returns default tools filtered by allowed actions
// Uses ToolManager to get registered tool definitions based on whitelist
func (h *Handler) getDefaultToolsWithFilter(allowedActions []string, agentConfig *config.AgentConfig) []interface{} {
	if h.ToolManager == nil {
		logger.Base().Warn("ToolManager not initialized, returning empty tools")
		return []interface{}{}
	}
	return h.ToolManager.GetInternalToolDefinitions(allowedActions)
}

// appendSystemTools adds enabled system tools to the tool list
func (h *Handler) appendSystemTools(tools []interface{}, agentConfig *config.AgentConfig) []interface{} {
	if agentConfig == nil || agentConfig.PromptConfig == nil {
		return tools
	}

	// Map of tool names to their condition checks and corresponding tool constants
	systemTools := []struct {
		name      string
		condition func() bool
	}{
		{
			name: toolspkg.ToolNameNotifyLanguageSwitch,
			condition: func() bool {
				return agentConfig.PromptConfig.IsAutoLanguageSwitchingEnabled()
			},
		},
		{
			name: toolspkg.ToolNameNotifyAccentChange,
			condition: func() bool {
				autoAccentEnabled := agentConfig.PromptConfig.IsAutoAccentAdaptationEnabled()
				hasConfiguredAccents := len(agentConfig.PromptConfig.LanguageInstructions) > 0
				return autoAccentEnabled || hasConfiguredAccents
			},
		},
	}

	for _, sysTool := range systemTools {
		// Check if feature is enabled
		if !sysTool.condition() {
			continue
		}

		// Check if tool is already in the list to prevent duplicates
		exists := false
		for _, t := range tools {
			if toolMap, ok := t.(map[string]interface{}); ok {
				if name, ok := toolMap["name"].(string); ok && name == sysTool.name {
					exists = true
					break
				}
			}
		}

		if exists {
			logger.Base().Warn("📌 System tool already in list, skipping", zap.String("name", sysTool.name))
			continue
		}

		// Add the tool
		if h.ToolManager != nil {
			defs := h.ToolManager.GetInternalToolDefinitions([]string{sysTool.name})
			if len(defs) > 0 {
				tools = append(tools, defs[0])
				logger.Base().Info("Auto-added system tool", zap.String("name", sysTool.name))
			}
		}
	}

	return tools
}

// getAgentVoiceAndSpeed retrieves voice and speed configuration from agent config
func (h *Handler) getAgentVoiceAndSpeed(agentID, channelType string) (string, float64) {
	if h.AgentConfigGetter == nil {
		logger.Base().Error("AgentConfigGetter not set, cannot retrieve agent-specific voice/speed")
		return "", 0
	}

	// Use background context and provided channel type
	agentConfig, err := h.AgentConfigGetter(context.Background(), agentID, channelType)
	if err != nil || agentConfig == nil {
		logger.Base().Warn("Agent config not found for agent", zap.String("agent_id", agentID))
		return "", 0
	}

	voice := agentConfig.Voice
	speed := agentConfig.Speed

	// Validate values
	if voice == "" {
		logger.Base().Info("📢 Agent has no voice configured, will use language-based default", zap.String("agent_id", agentID))
	}
	//openai limit speed to 0.25 to 1.5
	if speed <= 0.25 {
		speed = 0.25
	}
	if speed >= 1.5 {
		speed = 1.5
	}
	if voice != "" && speed > 0 {
		logger.Base().Info("Using agent configured voice", zap.String("agent_id", agentID), zap.String("voice", voice), zap.Float64("speed", speed))
	}

	return voice, speed
}
