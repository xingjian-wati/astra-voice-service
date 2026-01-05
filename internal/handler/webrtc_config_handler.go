package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ClareAI/astra-voice-service/internal/services/call"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// WebRTCConfigHandler handles WebRTC configuration endpoints
type WebRTCConfigHandler struct {
	service *call.WhatsAppCallService
}

// ICEServerConfig represents an ICE server configuration for the frontend
type ICEServerConfig struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// WebRTCConfigResponse represents the WebRTC configuration response
type WebRTCConfigResponse struct {
	ICEServers           []ICEServerConfig `json:"iceServers"`
	ICECandidatePoolSize int               `json:"iceCandidatePoolSize"`
}

// NewWebRTCConfigHandler creates a new WebRTC config handler
func NewWebRTCConfigHandler(service *call.WhatsAppCallService) *WebRTCConfigHandler {
	return &WebRTCConfigHandler{
		service: service,
	}
}

// SetupWebRTCConfigRoutes sets up routes for WebRTC configuration
func (h *WebRTCConfigHandler) SetupWebRTCConfigRoutes(router *mux.Router) {
	router.HandleFunc("/api/webrtc/config", h.getWebRTCConfig).Methods("GET")
	router.HandleFunc("/api/webrtc/config", h.handleCORS).Methods("OPTIONS")
	logger.Base().Info("WebRTC config endpoint registered", zap.String("path", "/api/webrtc/config"))
}

// getWebRTCConfig godoc
// @Summary Get WebRTC configuration
// @Description Get ICE servers configuration including STUN and TURN servers with credentials
// @Tags webrtc
// @Accept json
// @Produce json
// @Success 200 {object} WebRTCConfigResponse "WebRTC configuration"
// @Router /api/webrtc/config [get]
func (h *WebRTCConfigHandler) getWebRTCConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	config := &WebRTCConfigResponse{
		ICEServers:           make([]ICEServerConfig, 0),
		ICECandidatePoolSize: 10,
	}

	// Add STUN servers from configuration
	stunServers := h.service.GetSTUNServers()
	for _, stunURL := range stunServers {
		config.ICEServers = append(config.ICEServers, ICEServerConfig{
			URLs: []string{stunURL},
		})
	}

	// Add TURN servers from Twilio (dynamic)
	turnCredentials := h.service.GetTURNCredentials()
	for _, cred := range turnCredentials {
		config.ICEServers = append(config.ICEServers, ICEServerConfig{
			URLs:       cred.URLs,
			Username:   cred.Username,
			Credential: cred.Credential,
		})
	}

	logger.Base().Info("WebRTC config requested", zap.Int("stun_servers", len(stunServers)), zap.Int("turn_servers", len(turnCredentials)))

	if err := json.NewEncoder(w).Encode(config); err != nil {
		logger.Base().Error("Failed to encode WebRTC config", zap.Error(err))
		http.Error(w, "Failed to encode configuration", http.StatusInternalServerError)
		return
	}
}

// handleCORS handles CORS for WebRTC config endpoint
func (h *WebRTCConfigHandler) handleCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)
}
