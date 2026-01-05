package gemini

import (
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// updateSessionInstructions dynamically updates Gemini's system instructions.
func (h *Handler) updateSessionInstructions(connectionID string, instructions string) {
	// Gemini does not support updating the model mid-session, but systemInstruction can be updated.
	// Documentation: You can change the configuration parameters except the model during the session.
	event := map[string]interface{}{
		"setup": map[string]interface{}{
			"systemInstruction": map[string]interface{}{
				"role": "system",
				"parts": []map[string]interface{}{
					{"text": instructions},
				},
			},
		},
	}

	if err := h.sendEvent(connectionID, event); err != nil {
		logger.Base().Error("Failed to update session instructions", zap.Error(err))
		return
	}

	// Cache instructions
	h.Mutex.Lock()
	h.SessionInstructions[connectionID] = instructions
	h.Mutex.Unlock()
}

// resetSessionInstructions resets Gemini's system instructions.
func (h *Handler) resetSessionInstructions(connectionID string) {
	// Implementation for resetting instructions
}
