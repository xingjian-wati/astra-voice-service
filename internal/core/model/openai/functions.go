package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentconfig "github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/internal/core/tool"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/pkg/pubsub"
	"go.uber.org/zap"
)

// executeFunctionCall executes the function call and returns result to model
//
// Special Handling vs Standard Execution:
// Some tools require special handling before standard ToolManager execution:
//   - System-level operations (language switch, accent change) - handled here for direct OpenAI interaction
//   - Out-of-band responses (conversation summary) - need custom response.create with special metadata
//   - Business tools (booking, messaging) - use standard ToolManager execution flow
func (h *Handler) executeFunctionCall(connectionID, callID, functionName, arguments string) {
	modelConn, exists := h.GetConnection(connectionID)
	if !exists || modelConn == nil {
		logger.Base().Error("No model connection found for function call", zap.String("connection_id", connectionID))
		return
	}

	// Special handling for language switch notification
	if functionName == tool.ToolNameNotifyLanguageSwitch {
		h.handleLanguageSwitch(callID, connectionID, arguments)
		return
	}

	// Special handling for accent change notification
	if functionName == tool.ToolNameNotifyAccentChange {
		h.handleAccentChange(callID, connectionID, arguments)
		return
	}

	// Track active function call for silence/BGM gating.
	cleanup := h.MarkFunctionCallStart(connectionID)
	defer cleanup()

	// ========================================
	// Standard Execution: Business Tools
	// ========================================
	// All other tools use ToolManager for execution

	// Determine modality and get WhatsApp connection for logging
	modality := mcp.ModalityVoiceInbound
	var whatsappConn provider.CallConnection
	var isTestMode bool
	if h.ConnectionGetter != nil {
		whatsappConn = h.ConnectionGetter(connectionID)
		if whatsappConn != nil {
			if whatsappConn.GetIsOutbound() {
				modality = mcp.ModalityVoiceOutbound
			}
			isTestMode = whatsappConn.GetChannelTypeString() == string(domain.ChannelTypeTest)
		}
	}

	// Add system message BEFORE tool execution
	if whatsappConn != nil && isTestMode {
		whatsappConn.AddMessage(agentconfig.MessageRoleFunction, "I'm processing your request now. Please hold on for a moment.")
	}

	result, success := h.executeFunction(connectionID, functionName, arguments, modality)

	// Add system message AFTER tool execution
	if whatsappConn != nil && isTestMode {
		whatsappConn.AddMessage(agentconfig.MessageRoleFunction, "All set. I've completed that for you.")
	}

	// Cache action call/result for metrics (only for non-system tools)
	if whatsappConn != nil {
		whatsappConn.AddAction(pubsub.Action{
			ToolName: functionName,
			Param:    arguments,
			Result:   success,
		})
	}

	// Send function call result back to model
	h.sendFunctionResult(callID, result, connectionID)
}

// handleAccentChange handles accent change notification from AI
func (h *Handler) handleAccentChange(callID, connectionID, arguments string) {
	logger.Base().Info("🎭 Handling accent change for connection", zap.String("connection_id", connectionID))

	// Parse accent change parameters
	var params struct {
		Language string `json:"language"`
		Accent   string `json:"accent"`
	}
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		logger.Base().Error("Failed to parse arguments: (silently ignored)")
		h.sendFunctionResult(callID, `{"success": true, "message": "Continue with the conversation naturally. Focus on answering the user's question."}`, connectionID)
		return
	}

	language := params.Language
	accent := params.Accent

	if language == "" || accent == "" {
		logger.Base().Error("Language or accent parameter is empty (silently ignored)")
		h.sendFunctionResult(callID, `{"success": true, "message": "Continue with the conversation naturally. Focus on answering the user's question."}`, connectionID)
		return
	}

	// Validate and get instruction
	valid, errorMsg := h.validateAccentChange(connectionID, language, accent)

	var instruction string
	var resultMsg string

	if !valid {
		logger.Base().Error("Validation failed: (silently ignored, continuing conversation)", zap.String("errormsg", errorMsg))
		resultMsg = `{"success": true, "message": "Continue with the conversation naturally. Focus on answering the user's question."}`
		h.sendFunctionResult(callID, resultMsg, connectionID)
	} else {
		logger.Base().Info("Confirmed: with accent", zap.String("language", language), zap.String("accent", accent))
		languageName := agentconfig.GetLanguageName(language)
		if languageName == "" {
			languageName = language
		}
		languageInstruction := fmt.Sprintf("🌐 Language Switch: Now speaking %s.", languageName)
		accentInstruction := agentconfig.GetAccentDetailedInstruction(language, accent)
		if strings.TrimSpace(accentInstruction) == "" {
			accentInstruction = fmt.Sprintf("🔊 Accent instruction: Use the %s accent for %s.", accent, languageName)
		}
		instruction = strings.TrimSpace(strings.Join([]string{languageInstruction, accentInstruction}, "\n"))
		resultMsg = fmt.Sprintf("Accent updated to %s for %s", accent, language)
		h.SetCurrentLanguageAccent(connectionID, language, accent)
		h.sendInstructionAndResult(callID, connectionID, instruction, valid, resultMsg)
	}
}

// sendInstructionAndResult sends system message and function result
// Send system message first (so AI knows the new language), then send function result (to trigger response)
func (h *Handler) sendInstructionAndResult(callID, connectionID, instruction string, success bool, message string) {
	// 1. Send system message first (so AI knows the new language before generating response)
	sysMessage := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type": "message",
			"role": "system",
			"content": []map[string]interface{}{
				{
					"type": "input_text",
					"text": instruction,
				},
			},
		},
	}
	if err := h.sendEvent(connectionID, sysMessage); err != nil {
		logger.Base().Error("Failed to send instruction")
		h.sendFunctionResult(callID, `{"success": false, "error": "Failed to send instruction"}`, connectionID)
		return
	}

	// 2. Send function result (trigger AI response, system message already sent)
	var result string
	if success {
		result = fmt.Sprintf(`{"success": true, "message": "%s"}`, message)
	} else {
		result = fmt.Sprintf(`{"success": false, "error": "%s"}`, message)
	}
	h.sendFunctionResult(callID, result, connectionID)
}

// validateLanguageSwitch validates if the language switch request is allowed
func (h *Handler) validateLanguageSwitch(connectionID, language string) (bool, string) {
	agentConfig := h.getAgentConfig(connectionID)
	if agentConfig == nil || agentConfig.PromptConfig == nil {
		// No agent config - allow language switch
		return true, ""
	}

	// Check if auto language switching is enabled
	if !agentConfig.PromptConfig.IsAutoLanguageSwitchingEnabled() {
		return false, "Language switching is disabled for this agent."
	}

	return true, ""
}

func parseAccents(raw string) []string {
	var results []string
	for _, part := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		results = append(results, trimmed)
	}
	return results
}

// validateAccentChange validates if the accent change request is allowed
func (h *Handler) validateAccentChange(connectionID, language, accent string) (bool, string) {
	agentConfig := h.getAgentConfig(connectionID)
	if agentConfig == nil || agentConfig.PromptConfig == nil {
		// No agent config - just check if accent is valid
		if !agentconfig.IsValidAccent(language, accent) {
			return false, fmt.Sprintf("Invalid accent '%s' for %s", accent, language)
		}
		return true, ""
	}

	// Case 1: If a fixed accent is configured for this language (e.g., en: "india")
	configuredAccent, hasConfigured := agentConfig.PromptConfig.LanguageInstructions[language]
	if hasConfigured {
		allowedAccents := parseAccents(configuredAccent)
		if len(allowedAccents) == 0 {
			allowedAccents = []string{configuredAccent}
		}

		for _, allowed := range allowedAccents {
			if strings.EqualFold(strings.TrimSpace(allowed), accent) {
				return true, ""
			}
		}

		return false, fmt.Sprintf("Accent is fixed to [%s] for %s. Cannot change to %s.",
			strings.Join(allowedAccents, ", "), language, accent)
	}

	// Case 2: No accent configured for this language
	if !agentConfig.PromptConfig.IsAutoAccentAdaptationEnabled() {
		return false, "Accent switching is disabled for unconfigured languages."
	}
	// if !agentconfig.IsValidAccent(language, accent) {
	// 	availableAccents := agentconfig.GetAvailableAccents(language)
	// 	if len(availableAccents) > 0 {
	// 		return false, fmt.Sprintf("Invalid accent '%s'. Available: %s",
	// 			accent, strings.Join(availableAccents, ", "))
	// 	}
	// 	return false, fmt.Sprintf("No accents available for %s", language)
	// }

	return true, ""
}

// handleLanguageSwitch handles language switch notification from AI
func (h *Handler) handleLanguageSwitch(callID, connectionID, arguments string) {
	logger.Base().Info("🌐 Handling language switch for connection", zap.String("connection_id", connectionID))

	// Parse language parameter
	var params struct {
		Language string `json:"language"`
	}
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		logger.Base().Error("Failed to parse arguments: (silently ignored)")
		h.sendFunctionResult(callID, `{"success": true, "message": "Continue with the conversation naturally. Focus on answering the user's question."}`, connectionID)
		return
	}

	language := params.Language
	if language == "" {
		logger.Base().Error("Language parameter is empty (silently ignored)")
		h.sendFunctionResult(callID, `{"success": true, "message": "Continue with the conversation naturally. Focus on answering the user's question."}`, connectionID)
		return
	}

	// Validate and get instruction
	valid, errorMsg := h.validateLanguageSwitch(connectionID, language)
	languageName := agentconfig.GetLanguageName(language)

	var instruction string
	var resultMsg string

	if !valid {
		logger.Base().Error("Validation failed: (silently ignored, continuing conversation)", zap.String("errormsg", errorMsg))
		resultMsg = `{"success": true, "message": "Continue with the conversation naturally. Focus on answering the user's question."}`
		h.sendFunctionResult(callID, resultMsg, connectionID)
	} else {
		logger.Base().Info("Confirmed: ()", zap.String("language", language), zap.String("languagename", languageName))

		// Check if accent is configured for this language
		agentConfig := h.getAgentConfig(connectionID)
		if agentConfig != nil && agentConfig.PromptConfig != nil {
			if configuredAccent, hasConfigured := agentConfig.PromptConfig.LanguageInstructions[language]; hasConfigured {
				allowedAccents := parseAccents(configuredAccent)
				if len(allowedAccents) == 0 {
					allowedAccents = []string{strings.TrimSpace(configuredAccent)}
				}
				primaryAccent := allowedAccents[0]
				instruction = agentconfig.GetAccentDetailedInstruction(language, primaryAccent)
				h.SetCurrentLanguageAccent(connectionID, language, primaryAccent)
				if len(allowedAccents) > 1 {
					instruction = strings.TrimSpace(instruction) + fmt.Sprintf(`

🔁 Other allowed accents for this language: %s`, strings.Join(allowedAccents[1:], ", "))
				}
				logger.Base().Info("Using configured accent(s)", zap.String("language", language), zap.Strings("accents", allowedAccents))
			}
		}

		// No accent configured: use basic instruction
		if instruction == "" {
			instruction = fmt.Sprintf("🌐 Language Switch: Now speaking %s. Use natural pronunciation.", languageName)
		}
		h.SetCurrentLanguageAccent(connectionID, language, "")
		// Append instruction to prevent greeting repetition
		instruction += "\n⚠️ CRITICAL: Answer the user's last input directly. DO NOT repeat the greeting or self-introduction."

		resultMsg = fmt.Sprintf("Language switched to %s", languageName)
		h.sendInstructionAndResult(callID, connectionID, instruction, valid, resultMsg)
	}
}

// executeFunction is the unified entry point for all function executions
func (h *Handler) executeFunction(connectionID, functionName, arguments string, modality string) (string, bool) {
	logger.Base().Info("Executing function: (modality: )", zap.String("functionname", functionName), zap.String("modality", modality))

	// No API endpoint configured, use unified tool manager
	logger.Base().Info("📱 Function using unified tool manager", zap.String("functionname", functionName))

	if h.ToolManager == nil {
		logger.Base().Error("ToolManager not initialized")
		return `{"success": false, "error": "Tool manager not initialized"}`, false
	}
	result, err := h.ToolManager.ExecuteTool(functionName, arguments, connectionID, modality)
	if err != nil {
		logger.Base().Error("Tool execution failed")
		return fmt.Sprintf(`{"success": false, "error": "%s"}`, err.Error()), false
	}

	logger.Base().Info("Function executed successfully", zap.String("functionname", functionName))
	return result, true
}

// getAgentConfig retrieves agent configuration for the current connection
func (h *Handler) getAgentConfig(connectionID string) *agentconfig.AgentConfig {
	if h.AgentConfigGetter == nil {
		return nil
	}

	// Get agent ID from connection
	if h.ConnectionGetter != nil {
		if conn := h.ConnectionGetter(connectionID); conn != nil {
			agentID := conn.GetAgentID()
			// Use background context and connection channel type
			agent, err := h.AgentConfigGetter(context.Background(), agentID, conn.GetChannelTypeString())
			if err != nil {
				logger.Base().Error("Failed to get agent config")
				return nil
			}
			return agent
		}
	}

	return nil
}

// sendFunctionResult sends the function call result back to OpenAI
func (h *Handler) sendFunctionResult(callID, result, connectionID string) {
	functionOutput := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  result,
		},
	}

	if err := h.sendEvent(connectionID, functionOutput); err != nil {
		logger.Base().Error("Failed to send function output")
		return
	}

	logger.Base().Info("Function call result sent", zap.String("result", result))

	// Trigger response generation
	response := map[string]interface{}{
		"type": "response.create",
	}

	if err := h.sendEvent(connectionID, response); err != nil {
		logger.Base().Error("Failed to trigger response after function call")
	} else {
		logger.Base().Info("Triggered response generation after function call")
	}
}
