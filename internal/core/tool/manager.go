package tool

import (
	"context"
	"encoding/json"
	"fmt"

	agentconfig "github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// ========================================
// System Notification Tool Schemas
// ========================================
// These schemas are used for system-level notification tools
// that don't go through normal tool execution flow

// LanguageSwitchSchema defines the schema for language switch notification
var LanguageSwitchSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"language": map[string]interface{}{
			"type":        "string",
			"description": "The language code the user is now speaking. Use ISO 639-1 codes: 'en' for English, 'zh' for Mandarin Chinese, 'yue' for Cantonese, 'es' for Spanish, 'fr' for French, 'de' for German, etc.",
			"enum": []string{
				"en", "zh", "yue", "es", "fr", "de", "ja", "ko", "pt", "it", "ru", "ar", "hi", "th", "vi",
			},
		},
	},
	"required": []string{"language"},
}

// AccentChangeSchema defines the schema for accent change notification
var AccentChangeSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"language": map[string]interface{}{
			"type":        "string",
			"description": "The language code: 'en' for English, 'zh' for Mandarin, 'yue' for Cantonese, 'es' for Spanish, etc.",
		},
		"accent": map[string]interface{}{
			"type":        "string",
			"description": "The specific accent or dialect to use. Examples: For English - 'india', 'singapore', 'uk', 'us', 'australia'; For Chinese - 'mainland', 'taiwan', 'singapore'; For Cantonese - 'hongkong', 'guangdong'; For Spanish - 'spain', 'mexico', 'latin'",
		},
	},
	"required": []string{"language", "accent"},
}

// Tool name constants
const (
	ToolNameNotifyLanguageSwitch = "notify_language_switch"
	ToolNameNotifyAccentChange   = "notify_accent_change"
)

/*
Tool Manager - Simplified Registry Pattern

Architecture:
- tool_manager.go (this file): Core manager, registry, routing, registration
- booking_common.go: Shared booking utilities (parameter parsing, sanitization)
- tool_booking_*.go: Individual tool implementations

Benefits:
- Single source of truth: registerBuiltInTools() in this file
- Each tool in its own file for easy maintenance
- Minimal boilerplate - just register and implement
- Clear separation of concerns

To add a new tool (2 simple steps):

1. Register in registerBuiltInTools() below (~7 lines):
   m.RegisterTool(&ToolDefinition{
       Name:        "book_hotel",
       Description: "Books a hotel room",
       Parameters:  StandardBookingSchema,  // from booking_common.go
       TemplateName: "hotel",
       Executor:    m.ExecuteBookHotel,  // method value, auto-binds m
   })

2. Create tool_booking_hotel.go with Execute method (~30 lines):
   func (m *ToolManager) ExecuteBookHotel(toolName, templateName, argumentsJSON, connectionID string) (string, error) {
       params, _ := m.extractAndSanitizeParams(toolName, argumentsJSON, connectionID)
       // Your logic here - method has access to m and all its methods
       return SuccessMessage, nil
   }

That's it! No factory methods, no extra files needed.
*/

// ToolExecutorFunc defines the function signature for tool execution
// The tool name and template name will be passed as parameters to ensure consistency
type ToolExecutorFunc func(toolName string, templateName string, argumentsJSON string, connectionID string) (string, error)

// ToolDefinition defines a tool with its metadata and execution logic
type ToolDefinition struct {
	Name         string                 // Tool name (e.g., "book_wati_demo")
	Description  string                 // Tool description for OpenAI
	Parameters   map[string]interface{} // OpenAI function parameters schema
	TemplateName string                 // Template name for booking template mapping
	Executor     ToolExecutorFunc       // Execution function
}

// Note: If an executor needs access to tool metadata (Name, TemplateName, etc.),
// it will be passed as a parameter to ToolExecutorFunc. For example:
//   Executor: func(toolName, templateName, args, connID string) (string, error) {
//       // templateName is passed as a parameter, no need to capture from registration
//       return m.ExecuteWithMetadata(toolName, templateName, args, connID)
//   }

// ToolManager manages tool definitions, routing, and execution
type ToolManager struct {
	ConnectionGetter func(connectionID string) ToolConnection
	registry         map[string]*ToolDefinition // Tool registry
	ComposioService  *mcp.ComposioService       // Optional MCP service
}

// ToolConnection provides connection information for tool execution
type ToolConnection interface {
	GetFrom() string
	GetContactName() string
	GetTenantID() string
	GetBusinessNumber() string
	GetAgentID() string     // Added GetAgentID for MCP support
	GetTextAgentID() string // Added GetTextAgentID for MCP support
	GetChannelType() string // Added GetChannelType for MCP support
}

// NewToolManager creates a new tool manager instance
func NewToolManager() *ToolManager {
	m := &ToolManager{
		registry: make(map[string]*ToolDefinition),
	}
	// Register all built-in tools inline
	m.registerBuiltInTools()
	return m
}

// registerBuiltInTools registers all built-in tools
// This is the SINGLE place to add new tools - just add one entry here!
//
// Usage:
//   - With custom executor: Executor: m.YourCustomExecutor
//   - With default executor: Executor: nil (or omit the field)
//
// Example with default executor:
//
//	m.RegisterTool(&ToolDefinition{
//	    Name:        "book_new_service",
//	    Description: "Book a new service",
//	    Parameters:  StandardBookingSchema,
//	    TemplateName: "new_service",
//	    // Executor: nil (omitted) - will use default booking executor
//	})
func (m *ToolManager) registerBuiltInTools() {

	// Register language switch notification tool
	// Note: This tool has special handling in functions.go (handleLanguageSwitch)
	// and does not use the standard executor pattern
	m.RegisterTool(&ToolDefinition{
		Name:         ToolNameNotifyLanguageSwitch,
		Description:  "⚠️ CRITICAL: REQUIRED. Notify the system IMMEDIATELY when the user switches to a DIFFERENT LANGUAGE (not accent). ⚠️ LANGUAGE vs ACCENT: English with Indian accent is still ENGLISH (use notify_accent_change), Hindi is HINDI (use this function). Call this ONLY when the actual language changes (e.g., English->Hindi, English->Chinese), NOT when accent changes (e.g., American English->Indian English). Example: User speaks Hindi -> call notify_language_switch(language=\"hi\"). Example: User speaks English with Indian accent -> use notify_accent_change instead, NOT this function.",
		Parameters:   LanguageSwitchSchema,
		TemplateName: "",  // No template needed for system notifications
		Executor:     nil, // Special handling in functions.go
	})

	// Register accent change notification tool
	// Note: This tool has special handling in functions.go (handleAccentChange)
	// and does not use the standard executor pattern
	m.RegisterTool(&ToolDefinition{
		Name:         ToolNameNotifyAccentChange,
		Description:  "⚠️ CRITICAL: You MUST actively detect the user's accent on EVERY message. Call this function IMMEDIATELY when: (1) You detect an accent for the first time, (2) The user's accent changes from the current one, (3) The user explicitly requests a specific accent, or (4) the new language is spoken with a regional accent (notify_accent_change changes both language and accent). Listen to pronunciation patterns, intonation, rhythm, and speech characteristics. Examples: First time detecting Indian accent → call notify_accent_change(language=\"en\", accent=\"india\"). User switches from American to Indian accent → call notify_accent_change(language=\"en\", accent=\"india\"). User switches from Chinese to English with Indian accent → call notify_accent_change(language=\"en\", accent=\"india\") without a separate notify_language_switch call. User asks \"use British accent\" → call notify_accent_change(language=\"en\", accent=\"uk\"). Do NOT call if accent remains the same. After calling, you will receive accent instructions.",
		Parameters:   AccentChangeSchema,
		TemplateName: "",  // No template needed for system notifications
		Executor:     nil, // Special handling in functions.go
	})

	// ========================================
	// Examples: Add more tools with default executors
	// ========================================

	// m.RegisterTool(&ToolDefinition{
	// 	Name:        "book_hotel_reservation",
	// 	Description: "Send a WhatsApp booking template for hotel reservation",
	// 	Parameters:  BookingWithServiceSchema,
	// 	TemplateName: "hotel",
	// 	Executor:    m.ExecuteDefaultBooking,
	// })

	// m.RegisterTool(&ToolDefinition{
	// 	Name:        "send_order_confirmation",
	// 	Description: "Send order confirmation message to customer",
	// 	Parameters:  SendMessageSchema,
	// 	TemplateName: "ecommerce",
	// 	Executor:    m.ExecuteDefaultSendMessage,
	// })
}

// RegisterTool registers a custom tool
func (m *ToolManager) RegisterTool(tool *ToolDefinition) {
	m.registry[tool.Name] = tool
	logger.Base().Info("Registered tool", zap.String("name", tool.Name))
}

// GetInternalToolDefinitions returns the function tool definitions for OpenAI
// Builds definitions dynamically from registered tools in the registry
// If allowedActions is provided, only returns tools that are in the allowed list
// If a tool in allowedActions doesn't exist in registry, dynamically creates it with default booking logic (without registering)
// If allowedActions is empty, returns empty tools (whitelist mode)
func (m *ToolManager) GetInternalToolDefinitions(allowedActions []string) []interface{} {
	// If no allowedActions specified, return empty tools (whitelist mode)
	if len(allowedActions) == 0 {
		logger.Base().Info("🔒 No allowedActions specified, returning empty tools (whitelist mode)")
		return []interface{}{}
	}

	// With allowedActions specified, process each action dynamically (without registering)
	logger.Base().Info("Filtering tools by allowed actions", zap.Strings("allowed_actions", allowedActions))
	tools := make([]interface{}, 0, len(allowedActions))

	for _, actionName := range allowedActions {
		// Check if tool exists in registry
		var toolName, toolDescription string
		var toolParameters map[string]interface{}

		if tool, exists := m.registry[actionName]; exists {
			// Use registered tool
			toolName = tool.Name
			toolDescription = tool.Description
			toolParameters = tool.Parameters
			logger.Base().Info("Including registered tool", zap.String("actionname", actionName))
		} else {
			logger.Base().Warn("Tool not found in registry", zap.String("actionname", actionName))
			continue
		}

		// Build tool definition for OpenAI Realtime API (Flat structure)
		toolDef := map[string]interface{}{
			"type":        "function",
			"name":        toolName,
			"description": toolDescription,
			"parameters":  toolParameters,
		}
		tools = append(tools, toolDef)
	}

	logger.Base().Info("Returning tools", zap.Int("allowed_actions", len(allowedActions)), zap.Int("registry_count", len(m.registry)))
	return tools
}

// GetMcpToolDefinitions fetches and returns tool definitions from the MCP service
func (m *ToolManager) GetMcpToolDefinitions(ctx context.Context, agentID string, mode string, modality string) ([]interface{}, error) {
	if m.ComposioService == nil {
		return nil, fmt.Errorf("ComposioService not initialized")
	}

	mcpTools, err := m.ComposioService.ListTools(ctx, agentID, mode, modality)
	if err != nil {
		return nil, err
	}

	var tools []interface{}
	for _, mcpTool := range mcpTools {
		// Build tool definition for OpenAI Realtime API (Flat structure)
		tool := map[string]interface{}{
			"type":        "function",
			"name":        mcpTool.Name,
			"description": mcpTool.Description,
			"parameters":  mcpTool.InputSchema,
		}
		tools = append(tools, tool)
	}

	return tools, nil
}

// ExecuteTool is the unified entry point for all tool executions
// Routes to the appropriate executor based on tool registration
// If tool is not registered, uses default booking executor
func (m *ToolManager) ExecuteTool(toolName string, argumentsJSON string, connectionID string, modality string) (string, error) {
	logger.Base().Info("ExecuteTool called: for connection", zap.String("toolname", toolName), zap.String("connection_id", connectionID))

	// Try executing with MCP first
	if m.ComposioService == nil {
		return "", fmt.Errorf("ComposioService not initialized")
	}

	// Get agent ID from connection
	agentID := ""
	mode := agentconfig.AgentConfigModePublished // Default mode
	if modality == "" {
		modality = mcp.ModalityVoiceInbound // Default modality
	}

	if m.ConnectionGetter != nil {
		if conn := m.ConnectionGetter(connectionID); conn != nil {
			agentID = conn.GetAgentID()
			// Use TextAgentID if available for MCP calls
			if textAgentID := conn.GetTextAgentID(); textAgentID != "" {
				logger.Base().Info("Using TextAgentID for MCP call: (VoiceAgentID: )", zap.String("agent_id", agentID), zap.String("textagentid", textAgentID))
				agentID = textAgentID
			}

			// Determine mode based on channel type
			if conn.GetChannelType() == string(domain.ChannelTypeTest) {
				mode = agentconfig.AgentConfigModeDraft
			}
		}
	}

	if agentID == "" {
		return "", fmt.Errorf("agent ID not found for connection: %s", connectionID)
	}

	// Parse arguments
	var args map[string]interface{}
	if argumentsJSON != "" {
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			logger.Base().Error("Error parsing arguments for MCP tool")
		}
	} else {
		args = make(map[string]interface{})
	}

	argsJSON, _ := json.Marshal(args)
	logger.Base().Info("MCP tool call", zap.String("tool_name", toolName), zap.String("arguments", string(argsJSON)))

	// Call MCP tool
	mcpResult, err := m.ComposioService.CallToolMCP(context.Background(), agentID, mode, toolName, args, modality)
	if err != nil {
		logger.Base().Error("MCP tool execution skipped/failed: .")
		return "", fmt.Errorf("MCP tool execution failed: %w", err)
	}

	if mcpResult == nil {
		return "", fmt.Errorf("MCP tool executed but returned nil result")
	}

	logger.Base().Info("MCP tool executed successfully", zap.String("toolname", toolName))
	// Convert MCP result to string
	if len(mcpResult.Content) > 0 {
		// Return the text content of the first result item
		// TODO: Handle multiple content items or different types
		for _, content := range mcpResult.Content {
			if content.Type == "text" {
				return content.Text, nil
			}
		}
		// If no text content, try to marshal the whole result
		resultBytes, _ := json.Marshal(mcpResult)
		return string(resultBytes), nil
	}
	return `{"success": true, "message": "Action completed"}`, nil
}
