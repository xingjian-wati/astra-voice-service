package gemini

import (
	"fmt"

	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// sendInitialGreeting sends the initial greeting.
func (h *Handler) sendInitialGreeting(connectionID string) error {
	lang, _ := h.GetCurrentLanguageAccent(connectionID)
	return h.sendInitialGreetingWithLanguage(connectionID, lang)
}

// sendInitialGreetingWithLanguage sends the initial greeting with a specific language.
func (h *Handler) sendInitialGreetingWithLanguage(connectionID string, language string) error {
	logger.Base().Info("Sending initial greeting", zap.String("connection_id", connectionID), zap.String("language", language))

	// Get greeting text via prompt generator if available
	var text string
	if h.PromptGenerator != nil && h.ConnectionGetter != nil {
		if promptGen := h.PromptGenerator(connectionID); promptGen != nil {
			if conn := h.ConnectionGetter(connectionID); conn != nil {
				contactName := conn.GetContactName()
				contactNumber := conn.GetFrom()
				accent := conn.GetAccent()
				if conn.GetIsOutbound() {
					text = promptGen.GenerateOutboundGreeting(contactName, contactNumber, language, accent)
				} else {
					text = promptGen.GenerateGreetingInstruction(contactName, contactNumber, language, accent)
				}
			}
		}
	}

	if text == "" {
		logger.Base().Warn("No greeting text found", zap.String("connection_id", connectionID), zap.String("language", language))
		return nil
	}

	// Gemini uses clientContent to trigger AI speech
	event := map[string]interface{}{
		"clientContent": map[string]interface{}{
			"turns": []map[string]interface{}{
				{
					"role": "user",
					"parts": []map[string]interface{}{
						{"text": text},
					},
				},
			},
			"turnComplete": true,
		},
	}

	return h.sendEvent(connectionID, event)
}

// sendInactivityMessage sends an inactivity timeout message.
func (h *Handler) sendInactivityMessage(connectionID string, message string) {
	if message == "" {
		return
	}

	event := map[string]interface{}{
		"clientContent": map[string]interface{}{
			"turns": []map[string]interface{}{
				{
					"role": "user",
					"parts": []map[string]interface{}{
						{"text": message},
					},
				},
			},
			"turnComplete": true,
		},
	}

	h.sendEvent(connectionID, event)
}

// sendExitMessage sends a call termination message.
func (h *Handler) sendExitMessage(connectionID string, reason provider.ExitReason) {
	message := ""
	switch reason {
	case provider.ExitReasonTimeout:
		message = "We've reached the call time limit. We will end the call. You're welcome to contact us again anytime."
	case provider.ExitReasonSilence:
		message = "We haven't heard from you for a while. We will end the call. If you need help, please call us again anytime."
	default:
		message = "We will end the call. Please feel free to contact us again anytime."
	}

	event := map[string]interface{}{
		"clientContent": map[string]interface{}{
			"turns": []map[string]interface{}{
				{
					"role": "user",
					"parts": []map[string]interface{}{
						{"text": message},
					},
				},
			},
			"turnComplete": true,
		},
	}

	h.sendEvent(connectionID, event)
}

// sendEvent is a unified entry point for sending events.
func (h *Handler) sendEvent(connectionID string, event interface{}) error {
	conn, exists := h.GetConnection(connectionID)
	if !exists {
		return fmt.Errorf("connection not found: %s", connectionID)
	}

	if eventMap, ok := event.(map[string]interface{}); ok {
		return conn.SendEvent(eventMap)
	}

	return fmt.Errorf("invalid event type for Gemini")
}
