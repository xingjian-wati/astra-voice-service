package openai

import (
	"time"

	webrtcadapter "github.com/ClareAI/astra-voice-service/internal/adapters/webrtc"
	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
)

// ConversationMessage is an alias for config.ConversationMessage to maintain backward compatibility
type ConversationMessage = config.ConversationMessage

// PionOpusWriter is an alias for webrtc.OpusWriter to maintain backward compatibility
type PionOpusWriter = webrtcadapter.OpusWriter

// WhatsAppCallConnection is an alias for provider.CallConnection to maintain backward compatibility
type WhatsAppCallConnection = provider.CallConnection

// SpeechTiming is an alias for provider.SpeechTiming to maintain backward compatibility
type SpeechTiming = provider.SpeechTiming

// Handler manages AI model connections via WebRTC (supports multiple providers: OpenAI, Gemini, etc.)
type Handler struct {
	*provider.BaseHandler
}

// NewOpenAIHandler creates a new model handler with default OpenAI provider
func NewOpenAIHandler(cfg *config.WebSocketConfig) *Handler {
	return NewOpenAIHandlerWithProvider(cfg, NewProvider(cfg))
}

// NewOpenAIHandlerWithProvider creates a new model handler with a specific provider
func NewOpenAIHandlerWithProvider(cfg *config.WebSocketConfig, p provider.ModelProvider) *Handler {
	h := &Handler{
		BaseHandler: provider.NewBaseHandler(cfg, p),
	}

	// Set up BaseHandler callbacks
	h.OnInactivityTimeout = h.sendInactivityMessage
	h.OnExitTimeout = h.sendExitMessage
	h.OnSendInitialGreeting = h.sendInitialGreeting

	return h
}

// sendInactivityMessage sends the inactivity message to the user
func (h *Handler) sendInactivityMessage(connectionID string, message string) {
	instructions := h.BaseHandler.BuildLanguageAccentInstructions(connectionID, message)

	// Create a fixed assistant message item to avoid hallucination, then trigger TTS with response.create.
	itemEvent := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type": "message",
			"role": config.MessageRoleAssistant,
			"content": []map[string]interface{}{
				{
					"type": "output_text",
					"text": message,
				},
			},
		},
	}
	if err := h.sendEvent(connectionID, itemEvent); err != nil {
		logger.Base().Error("Failed to send inactivity item")
		return
	}

	responseEvent := map[string]interface{}{
		"type": "response.create",
		"response": map[string]interface{}{
			"instructions": instructions,
		},
	}
	if err := h.sendEvent(connectionID, responseEvent); err != nil {
		logger.Base().Error("Failed to trigger response for inactivity message")
	}
}

// sendExitMessage sends a polite exit notice in the current conversation language, waits 3s, then closes.
// reason: "timeout" or "silence"
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

	instructions := h.BaseHandler.BuildLanguageAccentInstructions(connectionID, message)

	// Inject assistant message item first to avoid hallucination, then trigger a minimal response.create.
	itemEvent := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type": "message",
			"role": config.MessageRoleAssistant,
			"content": []map[string]interface{}{
				{
					"type": "output_text",
					"text": message,
				},
			},
		},
	}
	if err := h.sendEvent(connectionID, itemEvent); err != nil {
		logger.Base().Error("Failed to send exit item")
		return
	}

	responseEvent := map[string]interface{}{
		"type": "response.create",
		"response": map[string]interface{}{
			"instructions": instructions,
		},
	}
	if err := h.sendEvent(connectionID, responseEvent); err != nil {
		logger.Base().Error("Failed to send exit message")
	}

	time.Sleep(5 * time.Second)
	h.CloseConnection(connectionID)
}
