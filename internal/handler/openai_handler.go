package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	whatsappconfig "github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/event"
	"github.com/ClareAI/astra-voice-service/internal/core/model/openai"
	"github.com/ClareAI/astra-voice-service/internal/core/tool"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/internal/prompts"
	"github.com/ClareAI/astra-voice-service/internal/services/agent"
	"github.com/ClareAI/astra-voice-service/internal/services/call"
	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/pkg/rag"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// OpenAIHandler handles OpenAI-related HTTP requests and configuration
type OpenAIHandler struct {
	config        *whatsappconfig.WhatsAppCallConfig
	openaiHandler *openai.Handler
	tokenHandler  *openai.RealtimeTokenHandler
	agentService  *agent.AgentService
}

// NewOpenAIHandler creates a new OpenAI handler with full configuration
func NewOpenAIHandler(cfg *whatsappconfig.WhatsAppCallConfig, service *call.WhatsAppCallService, agentService *agent.AgentService, composioService *mcp.ComposioService, openaiHandler *openai.Handler, tokenHandler *openai.RealtimeTokenHandler) *OpenAIHandler {
	// Create the handler instance
	handler := &OpenAIHandler{
		config:        cfg,
		openaiHandler: openaiHandler,
		tokenHandler:  tokenHandler,
		agentService:  agentService,
	}

	// Configure OpenAI handler with all dependencies
	handler.configureOpenAIHandler(service, composioService)

	return handler
}

// configureOpenAIHandler sets up all OpenAI handler dependencies
func (h *OpenAIHandler) configureOpenAIHandler(service *call.WhatsAppCallService, composioService *mcp.ComposioService) {
	// Set up dependencies for the OpenAI handler

	translator := rag.NewDefaultTranslator()

	// Use AgentService directly (unified entry point)
	agentRAGProcessor := rag.NewAgentRAGProcessor(h.agentService, translator)
	promptManager := prompts.NewMultiAgentPromptManager(h.agentService)

	// Set up function dependencies
	h.openaiHandler.TokenGenerator = func(sessionType, model, voice, language string, speed float64, tools []interface{}) (string, error) {
		tokenReq := openai.EphemeralTokenRequest{
			Session: openai.SessionConfig{
				Type:  sessionType,
				Model: model,
				Audio: openai.AudioConfig{
					Output: openai.AudioOutputConfig{
						Voice: voice,
						Speed: speed,
					},
				},
				Tools:    tools,
				Language: language,
			},
		}
		return h.tokenHandler.GenerateTokenInternal(tokenReq)
	}

	// Set up RAG processor that gets agent ID from connection
	h.openaiHandler.RAGProcessor = func(userInput, connectionID string) (bool, string, string) {
		// Get agent ID from connection
		conn := service.GetConnection(connectionID)
		if conn == nil {
			logger.Base().Warn("No connection found for RAG processing", zap.String("connection_id", connectionID))
			return false, "", userInput
		}

		agentID := conn.GetAgentID()
		channelTypeStr := conn.GetChannelTypeString()
		channelType := domain.ChannelType(channelTypeStr)
		return agentRAGProcessor.ProcessUserInputWithChannelType(userInput, connectionID, agentID, channelType)
	}

	h.openaiHandler.LanguageDetector = translator.DetectLanguage
	h.openaiHandler.ConnectionGetter = func(connectionID string) openai.WhatsAppCallConnection {
		conn := service.GetConnection(connectionID)
		if conn == nil {
			return nil
		}
		// Type assert to concrete type
		if callConn, ok := conn.(*call.WhatsAppCallConnection); ok {
			if callConn == nil {
				return nil
			}
			return callConn
		}
		return nil
	}

	// Set up event bus getter
	h.openaiHandler.EventBusGetter = func() event.EventBus {
		return service.GetEventBus()
	}

	// Initialize ComposioService
	// mcpConfig := config.LoadMCPServiceConfig()
	// composioService := pkg.NewComposioService(mcpConfig.MCPServiceURL)

	// Set up centralized tool manager
	toolManager := tool.NewToolManager()
	toolManager.ConnectionGetter = func(connectionID string) tool.ToolConnection {
		conn := service.GetConnection(connectionID)
		if conn == nil {
			return nil
		}
		// Type assert to concrete type
		if callConn, ok := conn.(*call.WhatsAppCallConnection); ok {
			// Adapt WhatsAppCallConnection to ToolConnection
			return &toolConnectionAdapter{conn: callConn}
		}
		return nil
	}
	// Set up ComposioService in tool manager
	toolManager.ComposioService = composioService

	h.openaiHandler.ToolManager = toolManager
	h.openaiHandler.PromptGenerator = func(connectionID string) whatsappconfig.PromptGenerator {
		// Get agent ID from connection
		conn := service.GetConnection(connectionID)
		if conn == nil {
			logger.Base().Warn("No connection found for prompt generation", zap.String("connection_id", connectionID))
			// Fallback to default agent
			defaultGenerator, _ := promptManager.GetPromptGenerator(whatsappconfig.GetDefaultAgentID())
			return defaultGenerator
		}
		agentID := conn.GetAgentID()
		channelTypeStr := conn.GetChannelTypeString()
		channelType := domain.ChannelType(channelTypeStr)
		promptGenerator, err := promptManager.GetPromptGeneratorWithChannelType(agentID, channelType)
		if err != nil {
			logger.Base().Error("Failed to load agent for connection : , using default", zap.String("connection_id", connectionID), zap.String("agent_id", agentID))
			defaultGenerator, _ := promptManager.GetPromptGenerator(whatsappconfig.GetDefaultAgentID())
			return defaultGenerator
		}

		return promptGenerator
	}

	// Set up agent config getter for voice and speed configuration
	// Use unified GetAgentConfigWithChannelType from agent service
	h.openaiHandler.AgentConfigGetter = func(ctx context.Context, agentID string, channelType string) (*whatsappconfig.AgentConfig, error) {
		return h.agentService.GetAgentConfigWithChannelType(ctx, agentID, domain.ChannelType(channelType))
	}

	logger.Base().Info("OpenAI handler configured successfully")
}

// GetOpenAIHandler returns the configured OpenAI handler
func (h *OpenAIHandler) GetOpenAIHandler() *openai.Handler {
	return h.openaiHandler
}

// SetupOpenAIRoutes sets up OpenAI-related routes
func (h *OpenAIHandler) SetupOpenAIRoutes(router *mux.Router) {
	// Setup OpenAI Realtime WebRTC routes using internal token handler
	router.HandleFunc("/realtime/token", h.tokenHandler.GenerateEphemeralToken).Methods("GET", "POST")
	router.HandleFunc("/realtime/token", h.tokenHandler.HandleCORS).Methods("OPTIONS")

	// Setup AI prompt generator endpoint
	router.HandleFunc("/api/agents/generate-prompt", h.GeneratePromptWithAI).Methods("POST")
	router.HandleFunc("/api/agents/generate-prompt", h.handleConfigCORS).Methods("OPTIONS")

	// Setup brandkit-based config generator endpoint
	router.HandleFunc("/api/agents/generate-from-brandkit", h.GenerateConfigFromBrandkit).Methods("POST")
	router.HandleFunc("/api/agents/generate-from-brandkit", h.handleConfigCORS).Methods("OPTIONS")

	logger.Base().Info("🤖 OpenAI routes registered (including AI generator and brandkit generator)")
}

// handleConfigCORS handles CORS for config endpoints
func (h *OpenAIHandler) handleConfigCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)
}

// ============================================================================
// AI Prompt Generator - Types and Functions
// ============================================================================

// GeneratePromptRequest represents the request body for prompt generation
type GeneratePromptRequest struct {
	Description        string                      `json:"description"`
	AgentContext       AgentContextInfo            `json:"agent_context"`
	Templates          GeneratePromptTemplateFlags `json:"templates"`
	GenerateFullConfig bool                        `json:"generate_full_config"` // Generate entire agent config
}

// AgentContextInfo contains current agent configuration
type AgentContextInfo struct {
	AgentName      string `json:"agent_name"`
	Persona        string `json:"persona"`
	Tone           string `json:"tone"`
	Language       string `json:"language"`
	Voice          string `json:"voice"`
	Services       string `json:"services"`
	Expertise      string `json:"expertise"`
	BusinessNumber string `json:"business_number"`
}

// GeneratePromptTemplateFlags specifies which templates to generate
type GeneratePromptTemplateFlags struct {
	Greeting           bool `json:"greeting"`
	Realtime           bool `json:"realtime"`
	SystemInstructions bool `json:"system_instructions"`
}

// GeneratePromptResponse represents the response with generated prompts
type GeneratePromptResponse struct {
	Prompts    *GeneratedPrompts     `json:"prompts,omitempty"`
	FullConfig *GeneratedAgentConfig `json:"full_config,omitempty"`
}

// GeneratedPrompts contains the generated prompt templates
type GeneratedPrompts struct {
	GreetingTemplate   string `json:"greeting_template,omitempty"`
	RealtimeTemplate   string `json:"realtime_template,omitempty"`
	SystemInstructions string `json:"system_instructions,omitempty"`
}

// GeneratedAgentConfig holds the complete generated agent configuration
type GeneratedAgentConfig struct {
	Persona            string   `json:"persona,omitempty"`
	Tone               string   `json:"tone,omitempty"`
	Language           string   `json:"language,omitempty"`
	DefaultAccent      string   `json:"default_accent,omitempty"`
	Voice              string   `json:"voice,omitempty"`
	Speed              float64  `json:"speed,omitempty"`
	Services           []string `json:"services,omitempty"`
	Expertise          []string `json:"expertise,omitempty"`
	GreetingTemplate   string   `json:"greeting_template,omitempty"`
	RealtimeTemplate   string   `json:"realtime_template,omitempty"`
	SystemInstructions string   `json:"system_instructions,omitempty"`
}

// openAIRequest represents request to OpenAI API
type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
}

// openAIMessage represents a message in OpenAI API
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponse represents response from OpenAI API
type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
}

// GeneratePromptWithAI generates prompt templates using OpenAI API
func (h *OpenAIHandler) GeneratePromptWithAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req GeneratePromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Description == "" {
		http.Error(w, "Description is required", http.StatusBadRequest)
		return
	}

	// Get OpenAI API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		logger.Base().Warn("OPENAI_API_KEY not set, cannot generate prompts")
		http.Error(w, "OpenAI API key not configured", http.StatusInternalServerError)
		return
	}

	// Check if we need to generate full config or just prompts
	var response GeneratePromptResponse

	if req.GenerateFullConfig {
		// Generate complete agent configuration
		fullConfig, err := h.generateFullAgentConfig(apiKey, req.Description)
		if err != nil {
			logger.Base().Error("Failed to generate full config")
			http.Error(w, fmt.Sprintf("Failed to generate configuration: %v", err), http.StatusInternalServerError)
			return
		}
		response.FullConfig = fullConfig
		logger.Base().Info("Generated full agent configuration")
	} else {
		// Generate individual prompts
		prompts := GeneratedPrompts{}

		if req.Templates.Greeting {
			greeting, err := h.generateGreetingPrompt(apiKey, req.Description, req.AgentContext)
			if err != nil {
				logger.Base().Error("Failed to generate greeting prompt")
			} else {
				prompts.GreetingTemplate = greeting
			}
		}

		if req.Templates.Realtime {
			realtime, err := h.generateRealtimePrompt(apiKey, req.Description, req.AgentContext)
			if err != nil {
				logger.Base().Error("Failed to generate realtime prompt")
			} else {
				prompts.RealtimeTemplate = realtime
			}
		}

		if req.Templates.SystemInstructions {
			systemInst, err := h.generateSystemInstructions(apiKey, req.Description, req.AgentContext)
			if err != nil {
				logger.Base().Error("Failed to generate system instructions")
			} else {
				prompts.SystemInstructions = systemInst
			}
		}

		response.Prompts = &prompts
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *OpenAIHandler) generateGreetingPrompt(apiKey, description string, context AgentContextInfo) (string, error) {
	systemPrompt := `You are an AI assistant that helps create greeting templates for voice agents. 
Based on the user's description and agent configuration, generate a natural, friendly greeting template that:
1. Introduces the agent and its purpose
2. Is warm and welcoming
3. Is concise (2-3 sentences)
4. Can include placeholders like {{contact_name}}, {{company_name}} if appropriate
5. Matches the specified tone and persona

Only return the greeting template text, nothing else.`

	contextInfo := h.buildContextString(context)
	userPrompt := fmt.Sprintf("Create a greeting template for this agent:\n\n%s\n\nAgent Configuration:\n%s", description, contextInfo)

	return h.callOpenAI(apiKey, systemPrompt, userPrompt)
}

func (h *OpenAIHandler) generateRealtimePrompt(apiKey, description string, context AgentContextInfo) (string, error) {
	systemPrompt := `You are an AI assistant that helps create realtime conversation templates for voice agents.
Based on the user's description and agent configuration, generate a comprehensive system prompt that:
1. Defines the agent's role and personality (matching the specified persona and tone)
2. Specifies conversation guidelines and tone
3. Lists key capabilities and services (from the provided services/expertise)
4. Includes any special instructions for handling conversations
5. Is detailed enough to guide the AI during real-time conversations
6. Should be in the specified language or multi-language if applicable

Only return the prompt template text, nothing else.`

	contextInfo := h.buildContextString(context)
	userPrompt := fmt.Sprintf("Create a realtime conversation template for this agent:\n\n%s\n\nAgent Configuration:\n%s", description, contextInfo)

	return h.callOpenAI(apiKey, systemPrompt, userPrompt)
}

func (h *OpenAIHandler) generateSystemInstructions(apiKey, description string, context AgentContextInfo) (string, error) {
	systemPrompt := `You are an AI assistant that helps create system-level instructions for voice agents.
Based on the user's description and agent configuration, generate clear, concise system instructions that:
1. Define core behavioral rules (aligned with the persona and tone)
2. Specify response format and style
3. List any constraints or limitations
4. Are brief and actionable (bullet points preferred)
5. Match the language and tone specifications

Only return the system instructions text, nothing else.`

	contextInfo := h.buildContextString(context)
	userPrompt := fmt.Sprintf("Create system instructions for this agent:\n\n%s\n\nAgent Configuration:\n%s", description, contextInfo)

	return h.callOpenAI(apiKey, systemPrompt, userPrompt)
}

func (h *OpenAIHandler) buildContextString(context AgentContextInfo) string {
	var parts []string

	if context.AgentName != "" {
		parts = append(parts, fmt.Sprintf("- Agent Name: %s", context.AgentName))
	}
	if context.Persona != "" {
		parts = append(parts, fmt.Sprintf("- Persona: %s", context.Persona))
	}
	if context.Tone != "" {
		parts = append(parts, fmt.Sprintf("- Tone: %s", context.Tone))
	}
	if context.Language != "" {
		parts = append(parts, fmt.Sprintf("- Language: %s", context.Language))
	}
	if context.Voice != "" {
		parts = append(parts, fmt.Sprintf("- Voice: %s", context.Voice))
	}
	if context.Services != "" {
		parts = append(parts, fmt.Sprintf("- Services: %s", context.Services))
	}
	if context.Expertise != "" {
		parts = append(parts, fmt.Sprintf("- Expertise: %s", context.Expertise))
	}
	if context.BusinessNumber != "" {
		parts = append(parts, fmt.Sprintf("- Business Number: %s", context.BusinessNumber))
	}

	if len(parts) == 0 {
		return "No specific configuration provided"
	}

	return joinStrings(parts, "\n")
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

func (h *OpenAIHandler) generateFullAgentConfig(apiKey, description string) (*GeneratedAgentConfig, error) {
	systemPrompt := `You are an AI assistant that generates complete voice agent configurations.
Based on the user's description, generate a comprehensive agent configuration in JSON format that includes:
1. persona - The agent's character and role (string)
2. tone - Communication style (string: friendly/professional/casual/empathetic/warm)
3. language - Primary language code (string: en/zh/es/fr/ja/de/pt/it/ru/ar)
4. voice - OpenAI voice name (string: alloy/ash/ballad/coral/echo/sage/shimmer/verse/marin/cedar)
5. speed - Speech speed (number: 0.5-2.0, default 1.0)
6. services - List of services offered (array of strings)
7. expertise - Areas of expertise (array of strings)
8. greeting_template - Initial greeting message (string)
9. realtime_template - Main conversation system prompt (string)
10. system_instructions - Core behavioral rules (string)

IMPORTANT: Return ONLY a valid JSON object with these exact field names. No markdown, no explanations.

Example format:
{
  "persona": "Professional customer service representative",
  "tone": "friendly",
  "language": "en",
  "voice": "coral",
  "speed": 1.0,
  "services": ["Product consultation", "Technical support"],
  "expertise": ["Sales", "Customer service"],
  "greeting_template": "Hello! I'm Sarah...",
  "realtime_template": "You are a professional...",
  "system_instructions": "- Always be polite..."
}`

	userPrompt := fmt.Sprintf("Generate a complete agent configuration for:\n\n%s", description)

	responseText, err := h.callOpenAI(apiKey, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	// Clean markdown code blocks if present
	cleanedJSON := cleanMarkdownJSON(responseText)

	// Parse JSON response
	var config GeneratedAgentConfig
	if err := json.Unmarshal([]byte(cleanedJSON), &config); err != nil {
		logger.Base().Error("Failed to parse JSON, raw response", zap.String("responsetext", responseText))
		logger.Base().Warn("Cleaned JSON", zap.String("cleanedjson", cleanedJSON))
		return nil, fmt.Errorf("failed to parse agent config JSON: %w", err)
	}

	return &config, nil
}

func (h *OpenAIHandler) callOpenAI(apiKey, systemPrompt, userPrompt string) (string, error) {
	// Delegate to the shared package function
	return CallOpenAI(apiKey, systemPrompt, userPrompt)
}

// ============================================================================
// Brandkit-based Config Generator
// ============================================================================

// BrandkitGenerateRequest represents the request for generating config from brandkit
type BrandkitGenerateRequest struct {
	TextAgentID string `json:"text_agent_id"`
}

// BrandkitResponse represents the response from brandkit API
type BrandkitResponse struct {
	ID              int    `json:"id"`
	AgentID         string `json:"agent_id"`
	Name            string `json:"name"`
	Domain          string `json:"domain"`
	Description     string `json:"description"`
	LongDescription string `json:"longDescription"`
	Logos           []struct {
		Theme   string `json:"theme"`
		Formats []struct {
			Src    string `json:"src"`
			Format string `json:"format"`
		} `json:"formats"`
	} `json:"logos"`
	Colors []struct {
		Hex        string `json:"hex"`
		Type       string `json:"type"`
		Brightness int    `json:"brightness"`
	} `json:"colors"`
	AgentBrandkitConfig struct {
		AgentRoleDescription   string   `json:"agent_role_description"`
		WelcomeMessage         string   `json:"welcome_message"`
		ConversationalStarters []string `json:"conversational_starters"`
	} `json:"agent_brandkit_config"`
	Company struct {
		Industries []struct {
			Name string `json:"name"`
		} `json:"industries"`
		Location struct {
			City    string `json:"city"`
			Country string `json:"country"`
		} `json:"location"`
	} `json:"company"`
}

// GenerateConfigFromBrandkit godoc
// @Summary Generate agent config from brandkit
// @Description Fetch brandkit data from external API and generate agent configuration using AI
// @Tags agents
// @Accept json
// @Produce json
// @Param request body BrandkitGenerateRequest true "Brandkit generation request"
// @Success 200 {object} GeneratedAgentConfig "Generated agent configuration"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/generate-from-brandkit [post]
func (h *OpenAIHandler) GenerateConfigFromBrandkit(w http.ResponseWriter, r *http.Request) {
	// Parse request
	var req BrandkitGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.TextAgentID == "" {
		http.Error(w, "text_agent_id is required", http.StatusBadRequest)
		return
	}

	logger.Base().Info("🎨 Fetching brandkit for agent", zap.String("textagentid", req.TextAgentID))

	// Fetch brandkit data
	brandkit, err := FetchBrandkit(req.TextAgentID)
	if err != nil {
		logger.Base().Error("Failed to fetch brandkit")
		http.Error(w, fmt.Sprintf("Failed to fetch brandkit: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Base().Info("Successfully fetched brandkit for", zap.String("name", brandkit.Name))

	// Get OpenAI API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		logger.Base().Warn("OPENAI_API_KEY not set, cannot generate config")
		http.Error(w, "OpenAI API key not configured", http.StatusInternalServerError)
		return
	}

	// Generate agent config using AI
	config, err := GenerateConfigFromBrandkitData(apiKey, brandkit)
	if err != nil {
		logger.Base().Error("Failed to generate config from brandkit")
		http.Error(w, fmt.Sprintf("Failed to generate configuration: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Base().Info("Successfully generated agent config for", zap.String("name", brandkit.Name))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// fetchBrandkit fetches brandkit data from external API
// Deprecated: Use FetchBrandkit package function instead
func (h *OpenAIHandler) fetchBrandkit(agentID string) (*BrandkitResponse, error) {
	return FetchBrandkit(agentID)
}

// generateConfigFromBrandkitData generates agent config using AI based on brandkit data
// Deprecated: Use GenerateConfigFromBrandkitData package function instead
func (h *OpenAIHandler) generateConfigFromBrandkitData(apiKey string, brandkit *BrandkitResponse) (*GeneratedAgentConfig, error) {
	return GenerateConfigFromBrandkitData(apiKey, brandkit)
}

// toolConnectionAdapter adapts openaihandler.WhatsAppCallConnection to tools.ToolConnection
type toolConnectionAdapter struct {
	conn openai.WhatsAppCallConnection
}

func (a *toolConnectionAdapter) GetFrom() string {
	return a.conn.GetFrom()
}

func (a *toolConnectionAdapter) GetContactName() string {
	return a.conn.GetContactName()
}

func (a *toolConnectionAdapter) GetTenantID() string {
	return a.conn.GetTenantID()
}

func (a *toolConnectionAdapter) GetBusinessNumber() string {
	return a.conn.GetBusinessNumber()
}

func (a *toolConnectionAdapter) GetAgentID() string {
	return a.conn.GetAgentID()
}

func (a *toolConnectionAdapter) GetChannelType() string {
	return a.conn.GetChannelTypeString()
}

func (a *toolConnectionAdapter) GetTextAgentID() string {
	return a.conn.GetTextAgentID()
}
