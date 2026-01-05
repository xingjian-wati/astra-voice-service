package openai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/internal/prompts"
	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// SendInitialGreeting sends initial greeting to OpenAI
func (h *Handler) SendInitialGreeting(connectionID string) error {
	return h.SendInitialGreetingWithLanguage(connectionID, "")
}

// getGreetingText generates greeting text using prompt generator or falls back to default
func (h *Handler) getGreetingText(connectionID string, conn provider.CallConnection) string {
	if conn == nil {
		return prompts.WatiGreetingInstruction("", "")
	}

	// Get connection info
	contactName := conn.GetContactName()
	contactNumber := conn.GetFrom()
	isOutbound := conn.GetIsOutbound()
	language := conn.GetVoiceLanguage()
	accent := conn.GetAccent()

	if h.PromptGenerator != nil {
		promptGen := h.PromptGenerator(connectionID)
		if promptGen != nil {
			var greetingText string
			if isOutbound {
				// For outbound calls, use self-introduction greeting
				greetingText = promptGen.GenerateOutboundGreeting(contactName, contactNumber, language, accent)
				logger.Base().Info("", zap.String("greetingtext", greetingText))
			} else {
				// For inbound calls, use thank-you greeting
				greetingText = promptGen.GenerateGreetingInstruction(contactName, contactNumber, language, accent)
				logger.Base().Info("", zap.String("greetingtext", greetingText))
			}
			return greetingText
		}
	}
	// Fallback to original Wati instruction if no prompt generator is available
	return prompts.WatiGreetingInstruction(contactName, contactNumber)
}

// SendInitialGreetingWithLanguage sends language-specific initial greeting to OpenAI
func (h *Handler) SendInitialGreetingWithLanguage(connectionID, language string) error {
	// Get connection
	var conn provider.CallConnection
	if h.ConnectionGetter != nil {
		conn = h.ConnectionGetter(connectionID)
	}

	// Generate greeting instruction using prompt generator
	greetingText := h.getGreetingText(connectionID, conn)

	var sessionInstructions string
	if conn != nil && h.PromptGenerator != nil {
		promptGen := h.PromptGenerator(connectionID)
		if promptGen != nil {
			accent := conn.GetAccent()
			sessionInstructions = promptGen.GenerateSessionInstructions(conn.GetFrom(), language, accent, conn.GetIsOutbound())
		}
	}

	// Sanitize session instructions: replace ActionID and AtID with ActionName
	if sessionInstructions != "" {
		if agentConfig := h.getAgentConfig(connectionID); agentConfig != nil {
			sessionInstructions = h.sanitizeInstructions(sessionInstructions, agentConfig)
		}
	}

	// Send session.update event to set persistent session-level instructions
	// This includes: RealtimeTemplate, language rules, accent, contact info, phone rules, functions
	if sessionInstructions != "" {
		if err := h.updateSessionInstructions(connectionID, sessionInstructions); err != nil {
			return err
		}
	}
	logger.Base().Info("sessionInstructions", zap.String("sessioninstructions", sessionInstructions))

	// Create response to trigger AI to speak the greeting
	// Language information is already in session instructions, so just trigger the response
	// The greetingText is passed as transient instructions to ensure the model starts the conversation
	responseMessage := map[string]interface{}{
		"type": "response.create",
		"response": map[string]interface{}{
			"instructions": greetingText,
		},
	}
	if err := h.sendEvent(connectionID, responseMessage); err != nil {
		return fmt.Errorf("failed to trigger greeting response: %w", err)
	}

	contactName := ""
	if conn != nil {
		contactName = conn.GetContactName()
	}
	logger.Base().Info("Initial greeting sent for connection: (language: , contact: )", zap.String("language", language), zap.String("connection_id", connectionID), zap.String("contact_name", contactName))
	return nil
}

// updateSessionInstructions sends session.update event with instructions
func (h *Handler) updateSessionInstructions(connectionID, instructions string) error {
	if instructions == "" {
		return nil
	}

	sessionUpdateMessage := map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"type":         "realtime",
			"instructions": instructions,
		},
	}

	if err := h.sendEvent(connectionID, sessionUpdateMessage); err != nil {
		return fmt.Errorf("failed to send session update: %w", err)
	}

	// Cache session instructions for later restoration
	h.Mutex.Lock()
	h.SessionInstructions[connectionID] = instructions
	h.Mutex.Unlock()

	logger.Base().Info("Session instructions updated and cached for connection", zap.String("connection_id", connectionID))
	return nil
}

// resetSessionInstructions resets session-level instructions from cache
func (h *Handler) resetSessionInstructions(connectionID string) error {
	logger.Base().Info("Resetting session instructions for connection", zap.String("connection_id", connectionID))

	// Get cached session instructions
	h.Mutex.RLock()
	sessionInstructions, exists := h.SessionInstructions[connectionID]
	h.Mutex.RUnlock()

	if !exists || sessionInstructions == "" {
		logger.Base().Warn("No cached session instructions found for connection", zap.String("connection_id", connectionID))
		return nil
	}

	// Restore original instructions using the common method
	if err := h.updateSessionInstructions(connectionID, sessionInstructions); err != nil {
		return fmt.Errorf("failed to reset session instructions: %w", err)
	}

	// Clear pending reset flag
	h.Mutex.Lock()
	delete(h.PendingReset, connectionID)
	h.Mutex.Unlock()

	logger.Base().Info("Session instructions reset for connection", zap.String("connection_id", connectionID))
	return nil
}

// sendInitialGreeting sends the initial greeting for a connection
func (h *Handler) sendInitialGreeting(connectionID string) error {
	logger.Base().Info("Sending initial greeting for", zap.String("connection_id", connectionID))

	// Get connection to determine language
	var language string = "en" // default
	if h.ConnectionGetter != nil {
		if conn := h.ConnectionGetter(connectionID); conn != nil {
			if lang := conn.GetVoiceLanguage(); lang != "" {
				language = lang
			}
		}
	}

	// Send the greeting with detected/default language
	return h.SendInitialGreetingWithLanguage(connectionID, language)
}

// selectVoiceForLanguage selects the most appropriate voice for a given language
func (h *Handler) selectVoiceForLanguage(language string) string {
	//'alloy', 'ash', 'ballad', 'coral', 'echo', 'sage', 'shimmer', 'verse', 'marin', and 'cedar'
	switch language {
	case "yue", "zh-HK":
		// For Cantonese/Hong Kong, use nova which has better Chinese pronunciation
		return "coral" // Best for Chinese variants, more natural
	case "zh", "zh-CN":
		// For Mandarin Chinese, use nova for better Chinese pronunciation
		return "coral" // Better Chinese pronunciation than verse
	case "es", "es-ES":
		// For Spanish, use amber which has warm, natural tone
		return "ash" // Warm, engaging voice
	case "en", "en-US", "en-GB":
		// For English, use marin for natural conversation
		return "marin" // Natural, conversational
	default:
		// Default to alloy for other languages (most neutral)
		return "alloy" // Most neutral voice
	}
}

// getSpeedForLanguage returns the optimal speech speed for different languages
func (h *Handler) getSpeedForLanguage(language string) float64 {
	switch language {
	case "yue", "zh-HK":
		// Cantonese: Slower speed for better pronunciation and natural rhythm
		return 1.0 // 15% slower for tonal clarity
	case "zh", "zh-CN":
		// Mandarin: Slightly slower for clearer tonal pronunciation
		return 1.0 // 10% slower for better Chinese pronunciation
	case "es", "es-ES":
		// Spanish: Normal speed, Spanish flows naturally
		return 1.0
	case "en", "en-US", "en-GB":
		// English: Faster conversational speed (1.1x)
		return 1.1
	default:
		// Default to slightly slower for better clarity
		return 0.95
	}
}

// getRealTimeLanguageConfigWithContext generates real-time language configuration with context
func (h *Handler) getRealTimeLanguageConfigWithContext(connectionID, language string) string {
	// Get contact number from connection
	var contactNumber string
	if h.ConnectionGetter != nil {
		if conn := h.ConnectionGetter(connectionID); conn != nil {
			contactNumber = conn.GetFrom()
		}
	}

	// Generate real-time instruction using prompt generator or fallback
	var baseInstruction string
	if h.PromptGenerator != nil {
		promptGen := h.PromptGenerator(connectionID)
		if promptGen != nil {
			baseInstruction = promptGen.GenerateRealtimeInstruction(contactNumber)
		} else {
			baseInstruction = prompts.WatiRealTimeInstruction(contactNumber)
		}
	} else {
		baseInstruction = prompts.WatiRealTimeInstruction(contactNumber)
	}

	// Add language-specific context
	languageContext := fmt.Sprintf(`

🌐 LANGUAGE CONTEXT:
- Detected user language: %s
- Always respond in the user's language
- Maintain natural conversation flow
- Use appropriate cultural context for the language

📞 PHONE CALL OPTIMIZATION:
- Keep responses concise and conversational
- Avoid long monologues
- Ask follow-up questions to maintain engagement`, language)

	return baseInstruction + languageContext
}

// updateRealTimeLanguageInstructions updates the real-time language instructions for a connection
func (h *Handler) updateRealTimeLanguageInstructions(connectionID string) error {
	logger.Base().Info("📝 Updating real-time language instructions for", zap.String("connection_id", connectionID))

	// TODO: Implement dynamic language instruction updates
	return nil
}

// injectLanguageGuidance injects language guidance when RAG is not available
func (h *Handler) injectLanguageGuidance(connectionID, userInput string) error {
	// Minimal guidance - accent and language rules already in session
	languageGuidance := fmt.Sprintf(`[CONTEXT] User said: "%s"
📞 Keep responses brief.`, userInput)

	systemMessage := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type": "message",
			"role": config.MessageRoleSystem,
			"content": []map[string]interface{}{
				{
					"type": "input_text",
					"text": languageGuidance,
				},
			},
		},
	}
	return h.sendEvent(connectionID, systemMessage)
}

// injectRAGContext injects RAG context as a system message into the conversation
func (h *Handler) injectRAGContext(connectionID, ragContext string) error {
	// Minimal guidance - accent and language rules already in session
	enhancedContext := ragContext + `

📚 KNOWLEDGE GUIDELINES:
- Base your answers on the knowledge above
- Speak naturally, don't just read information
- If knowledge is insufficient, say so honestly
- Keep responses brief (phone conversation)
- Maintain language consistency`

	// Create system message with enhanced RAG context
	systemMessage := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type": "message",
			"role": config.MessageRoleSystem,
			"content": []map[string]interface{}{
				{
					"type": "input_text",
					"text": enhancedContext,
				},
			},
		},
	}

	if err := h.sendEvent(connectionID, systemMessage); err != nil {
		return fmt.Errorf("failed to inject RAG context: %w", err)
	}

	return nil
}

// processUserMessage processes user message for RAG and language guidance
func (h *Handler) processUserMessage(connectionID, text string) {
	// Language detection (silent)
	if h.LanguageDetector != nil {
		h.LanguageDetector(text)
	}

	// Process user input with RAG
	if h.RAGProcessor != nil {
		shouldCallRAG, ragContext, _ := h.RAGProcessor(text, connectionID)
		if shouldCallRAG {
			h.injectRAGContext(connectionID, ragContext)
			return
		}
	}
	// Fallback to language guidance if RAG is not available
	// h.injectLanguageGuidance(connectionID, text)
}

// sanitizeInstructions replaces ActionID and AtID with McpTool in session instructions
func (h *Handler) sanitizeInstructions(instructions string, agentConfig *config.AgentConfig) string {
	if h.ToolManager == nil || h.ToolManager.ComposioService == nil {
		logger.Base().Warn("ToolManager or ComposioService not initialized, skipping instruction sanitization")
		return instructions
	}

	// 1. Fetch MCP templates
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	templates, err := h.ToolManager.ComposioService.GetActionTemplates(ctx)
	if err != nil {
		logger.Base().Error("Failed to fetch action templates for sanitization")
		return instructions
	}

	// Map for quick lookup: AtID -> McpTool
	templateMap := make(map[string]string)
	for _, tmpl := range templates {
		templateMap[tmpl.AtID] = tmpl.McpTool
	}

	// Helper to replace IDs with McpTool
	replaceIDs := func(actions []mcp.IntegratedAction) {
		for _, action := range actions {
			// Check if we have a template match for this action's AtID
			if mcpTool, found := templateMap[action.AtID]; found && mcpTool != "" {
				// Replace ActionID and AtID with McpTool
				if action.ActionID != "" {
					instructions = strings.ReplaceAll(instructions, action.ActionID, mcpTool)
				}
				if action.AtID != "" {
					instructions = strings.ReplaceAll(instructions, action.AtID, mcpTool)
				}
				logger.Base().Info("🧹 Sanitized action: ->", zap.String("atid", action.AtID))
			}
		}
	}

	replaceIDs(agentConfig.IntegratedActions)
	replaceIDs(agentConfig.OutboundIntegratedActions)

	return instructions
}
