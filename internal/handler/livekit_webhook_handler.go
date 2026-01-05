package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/livekit/protocol/livekit"
	"go.uber.org/zap"
)

// HandleLiveKitWebhook processes LiveKit webhook events
func (h *LiveKitHandler) HandleLiveKitWebhook(w http.ResponseWriter, r *http.Request) {
	logger.Base().Debug("Received LiveKit webhook")

	// Read request body
	var event livekit.WebhookEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		logger.Base().Error("Failed to decode LiveKit webhook")
		// Try to extract basic event info
		bodyBytes := make([]byte, 0)
		r.Body.Read(bodyBytes)
		logger.Base().Debug("Raw body", zap.String("body", string(bodyBytes)))

		// Return 200 OK anyway to avoid LiveKit retries
		w.WriteHeader(http.StatusOK)
		return
	}

	logger.Base().Info("LiveKit Webhook", zap.String("event", event.Event), zap.String("room", event.Room.Name))

	// TODO: Verify webhook signature in production

	// Handle different event types
	switch event.Event {
	case "egress_started":
		h.handleEgressStarted(&event)
	case "egress_updated":
		h.handleEgressUpdated(&event)
	case "egress_ended":
		h.handleEgressEnded(&event)
	case "room_started":
		h.handleRoomStarted(&event)
	case "room_finished":
		h.handleRoomFinished(&event)
	case "participant_joined":
		h.handleParticipantJoined(&event)
	case "participant_left":
		h.handleParticipantLeft(&event)
	default:
		logger.Base().Debug("Unhandled LiveKit event", zap.String("event", event.Event))
	}

	w.WriteHeader(http.StatusOK)
}

// handleEgressStarted handles egress started events
func (h *LiveKitHandler) handleEgressStarted(event *livekit.WebhookEvent) {
	logger.Base().Info("Egress started", zap.String("egress_id", event.EgressInfo.EgressId), zap.String("room", event.Room.Name))
}

// handleEgressUpdated handles egress updated events
func (h *LiveKitHandler) handleEgressUpdated(event *livekit.WebhookEvent) {
	logger.Base().Info("Egress updated", zap.String("egress_id", event.EgressInfo.EgressId), zap.String("status", event.EgressInfo.Status.String()))
}

// handleEgressEnded handles egress ended events
func (h *LiveKitHandler) handleEgressEnded(event *livekit.WebhookEvent) {
	logger.Base().Info("Egress ended", zap.String("egress_id", event.EgressInfo.EgressId), zap.String("status", event.EgressInfo.Status.String()))

	if event.EgressInfo.FileResults != nil {
		for _, result := range event.EgressInfo.FileResults {
			logger.Base().Info("Egress file result", zap.String("filename", result.Filename), zap.Int64("size", result.Size))
		}
	}

	// TODO: Add business logic (download file, update database, send notification, etc.)

	if event.EgressInfo.Error != "" {
		logger.Base().Error("Egress failed", zap.String("error", event.EgressInfo.Error))
		// TODO: Handle recording failure (log, alert, retry, etc.)
	}
}

// handleRoomStarted handles room started events
func (h *LiveKitHandler) handleRoomStarted(event *livekit.WebhookEvent) {
	logger.Base().Info("Room started", zap.String("room", event.Room.Name))
}

// handleRoomFinished handles room finished events
func (h *LiveKitHandler) handleRoomFinished(event *livekit.WebhookEvent) {
	logger.Base().Info("Room finished", zap.String("room", event.Room.Name))

	// TODO: Cleanup related resources
}

// handleParticipantJoined handles participant joined events
func (h *LiveKitHandler) handleParticipantJoined(event *livekit.WebhookEvent) {
	logger.Base().Info("Participant joined", zap.String("participant", event.Participant.Identity), zap.String("room", event.Room.Name))
}

// handleParticipantLeft handles participant left events
func (h *LiveKitHandler) handleParticipantLeft(event *livekit.WebhookEvent) {
	logger.Base().Info("Participant left", zap.String("participant", event.Participant.Identity), zap.String("room", event.Room.Name))
}
