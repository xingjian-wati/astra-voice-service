package openai

import (
	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// handleResponseAudioTranscriptDone handles when assistant's audio transcript is done
func (h *Handler) handleResponseAudioTranscriptDone(connectionID string, event map[string]interface{}) {
	if transcript, ok := event["transcript"].(string); ok && transcript != "" {
		// Add to conversation history
		if h.ConnectionGetter != nil {
			if conn := h.ConnectionGetter(connectionID); conn != nil {
				conn.AddMessage(config.MessageRoleAssistant, transcript)
			}
		}

		logger.Base().Info("Added assistant transcript to history")

		// Check if this is the first assistant message (initial greeting)
		if h.ConnectionGetter != nil {
			if conn := h.ConnectionGetter(connectionID); conn != nil {
				conversationHistory := conn.GetConversationHistory()
				if len(conversationHistory) == 1 && conversationHistory[0].Role == config.MessageRoleAssistant {
					logger.Base().Info("finished initial greeting, VAD already active", zap.String("connection_id", connectionID))
					// Mark that greeting is finished and we can switch to realtime VAD handling
					conn.SetSwitchedToRealtime(true)
				}
			}
		}
	}
}
