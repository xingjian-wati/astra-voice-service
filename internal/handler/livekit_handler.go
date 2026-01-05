package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/adapters/livekit"
	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/internal/core/task"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/internal/services/agent"
	"github.com/ClareAI/astra-voice-service/internal/services/call"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// LiveKitHandler handles LiveKit-related HTTP requests
type LiveKitHandler struct {
	roomManager  *livekit.RoomManager
	service      *call.WhatsAppCallService // Shared service for compatibility
	agentService *agent.AgentService       // Agent service for agent verification
	taskBus      task.Bus                  // Task bus for asynchronous processing
}

// NewLiveKitHandler creates a new LiveKit handler
func NewLiveKitHandler(roomManager *livekit.RoomManager, service *call.WhatsAppCallService, taskBus task.Bus) *LiveKitHandler {
	agentService, err := agent.GetAgentService()
	if err != nil {
		logger.Base().Error("Failed to get agent service")
	}

	return &LiveKitHandler{
		roomManager:  roomManager,
		service:      service,
		agentService: agentService,
		taskBus:      taskBus,
	}
}

// CreateRoomRequest represents the request to create a LiveKit room
type CreateRoomRequest struct {
	ParticipantName string `json:"participantName"` // Required: participant display name
	AgentID         string `json:"agentId"`         // Optional: specific agent ID
	VoiceLanguage   string `json:"voiceLanguage"`   // Optional: voice language (e.g., "en", "zh")
	TenantID        string `json:"tenantId"`        // Optional: tenant ID for multi-tenancy
}

// CreateRoomResponse represents the response from creating a room
type CreateRoomResponse struct {
	ConnectionID string `json:"connectionId"` // Internal connection ID
	RoomName     string `json:"roomName"`     // LiveKit room name
	AccessToken  string `json:"accessToken"`  // JWT access token for client
	ServerURL    string `json:"serverUrl"`    // LiveKit server WebSocket URL
	Status       string `json:"status"`       // "created"
}

// JoinRoomRequest represents the request to join an existing room
type JoinRoomRequest struct {
	RoomName        string `json:"roomName"`        // Required: Room name to join
	ParticipantName string `json:"participantName"` // Required: Participant display name
	AgentID         string `json:"agentId"`         // Optional: specific agent ID
	VoiceLanguage   string `json:"voiceLanguage"`   // Optional: voice language (e.g., "en", "zh")
	TenantID        string `json:"tenantId"`        // Optional: tenant ID for multi-tenancy
}

// EndCallRequest represents the request to end a call
type EndCallRequest struct {
	ConnectionID string `json:"connectionId"` // Connection ID to end
}

// ConnectionStatusResponse represents the connection status
type ConnectionStatusResponse struct {
	ConnectionID  string    `json:"connectionId"`
	RoomName      string    `json:"roomName"`
	ParticipantID string    `json:"participantId"`
	IsActive      bool      `json:"isActive"`
	IsAIReady     bool      `json:"isAIReady"` // keep JSON tag for backward compatibility
	AgentID       string    `json:"agentId"`
	CreatedAt     time.Time `json:"createdAt"`
	LastActivity  time.Time `json:"lastActivity"`
}

// SetupLiveKitRoutes registers LiveKit routes
func (h *LiveKitHandler) SetupLiveKitRoutes(router *mux.Router) {
	// Create LiveKit subrouter with CORS middleware
	livekitRouter := router.PathPrefix("/livekit").Subrouter()
	livekitRouter.Use(CORSMiddleware)

	// POST /livekit/create-room - Create new room and get access token
	livekitRouter.HandleFunc("/create-room", h.HandleCreateRoom).Methods("POST", "OPTIONS")

	// POST /livekit/join-room - Join existing room (future use)
	livekitRouter.HandleFunc("/join-room", h.HandleJoinRoom).Methods("POST", "OPTIONS")

	// POST /livekit/end-call - End call and cleanup
	livekitRouter.HandleFunc("/end-call", h.HandleEndCall).Methods("POST", "OPTIONS")

	// GET /livekit/connection-status/:connectionId - Get connection status
	livekitRouter.HandleFunc("/connection-status/{connectionId}", h.HandleConnectionStatus).Methods("GET", "OPTIONS")

	// GET /livekit/stats - Get LiveKit statistics
	livekitRouter.HandleFunc("/stats", h.HandleStats).Methods("GET", "OPTIONS")

	// POST /livekit/webhook - LiveKit webhook endpoint (for egress_ended, etc.)
	livekitRouter.HandleFunc("/webhook", h.HandleLiveKitWebhook).Methods("POST")

	logger.Base().Info("🎙 LiveKit routes registered (including webhook endpoint)")
}

// HandleCreateRoom creates a new LiveKit room and returns connection info
// POST /livekit/create-room
func (h *LiveKitHandler) HandleCreateRoom(w http.ResponseWriter, r *http.Request) {
	logger.Base().Info("🎙 Received create room request")

	var request CreateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logger.Base().Error("Failed to decode request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if request.ParticipantName == "" {
		logger.Base().Error("Participant name is required")
		http.Error(w, "Participant name is required", http.StatusBadRequest)
		return
	}
	if request.AgentID == "" {
		logger.Base().Error("Agent ID is required")
		http.Error(w, "Agent ID is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if request.VoiceLanguage == "" {
		request.VoiceLanguage = config.DefaultLanguage
	}

	// Validate agent ID and get TextAgentID if available
	var textAgentID string
	if h.agentService != nil {
		agentConfig, err := h.agentService.GetAgentConfigWithChannelType(context.Background(), request.AgentID, domain.ChannelTypeLiveKit)
		if err != nil || agentConfig == nil {
			logger.Base().Error("Agent validation failed")
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		// Use Agent ID from config (handles validation)
		request.AgentID = agentConfig.ID
		textAgentID = agentConfig.TextAgentID
		logger.Base().Info("Agent verified", zap.String("agent_id", request.AgentID))
	}

	logger.Base().Info("🎙 Creating room for participant", zap.String("participant_name", request.ParticipantName), zap.String("agent_id", request.AgentID), zap.String("voice_language", request.VoiceLanguage))

	// Generate unique IDs
	connectionID := fmt.Sprintf("livekit-%d", time.Now().UnixNano())
	roomName := fmt.Sprintf("%s%s", config.DefaultRoomPrefix, connectionID)

	// Create WhatsAppCallConnection (reusing existing structure)
	connection := &call.WhatsAppCallConnection{
		ID:            connectionID,
		CallID:        roomName, // Use room name as call ID
		From:          request.ParticipantName,
		To:            fmt.Sprintf("%s%s", config.DefaultLiveKitBotPrefix, connectionID),
		CreatedAt:     time.Now(),
		LastActivity:  time.Now(),
		IsActive:      true,
		ChannelType:   domain.ChannelTypeLiveKit, // LiveKit channel doesn't need audio caching
		TenantID:      request.TenantID,
		AgentID:       request.AgentID,
		TextAgentID:   textAgentID,
		VoiceLanguage: request.VoiceLanguage,
	}

	// Register connection in service
	h.service.AddConnection(connection)

	// Initialize voice conversation for this connection
	if err := connection.InitializeVoiceConversation(); err != nil {
		logger.Base().Error("Failed to initialize voice conversation")
		// Continue anyway, AddMessage will create it as fallback
	}

	logger.Base().Info("Connection created", zap.String("connection_id", connectionID))

	// Generate LiveKit access token
	accessToken, err := h.roomManager.GenerateToken(roomName, request.ParticipantName)
	if err != nil {
		logger.Base().Error("Failed to generate token")
		h.service.CleanupConnection(connectionID)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Initialize AI model and join room in parallel (async)
	if h.taskBus != nil {
		logger.Base().Info("Enqueuing livekit-room task", zap.String("conn_id", connectionID))
		h.taskBus.Publish(context.Background(), task.SessionTask{
			Type:         task.TaskTypeLiveKitRoom,
			ConnectionID: connectionID,
			Payload:      nil, // Optional: could include room joining details
		})
		// We still need to join the room locally as a bot
		go h.roomManager.JoinRoomAsBot(connectionID, roomName, false)
	} else {
		// Fallback to local asynchronous processing
		go func() {
			// Enable greeting signal control (let Model handler wait for participant join before sending greeting)
			// Determine provider type
			providerType := provider.ProviderTypeOpenAI
			if connection.ModelProvider == provider.ProviderTypeGemini {
				providerType = provider.ProviderTypeGemini
			}

			if modelHandler, err := h.service.GetModelHandler(providerType); err == nil && modelHandler != nil {
				modelHandler.EnableGreetingSignalControl(connectionID)
				logger.Base().Info("Enabled greeting signal control", zap.String("connection_id", connectionID), zap.String("provider", string(providerType)))
			}

			// Start both operations simultaneously
			go h.service.InitializeAIConnection(connection)
			go h.roomManager.JoinRoomAsBot(connectionID, roomName, false) // false = wait for signal control
		}()
	}

	logger.Base().Info("Room created", zap.String("room_name", roomName))

	// Get server URL from connection config
	serverURL := h.roomManager.GetConfigInternal().ServerURL

	// Return response with access token and server URL
	response := CreateRoomResponse{
		ConnectionID: connectionID,
		RoomName:     roomName,
		AccessToken:  accessToken,
		ServerURL:    serverURL,
		Status:       "created",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

	logger.Base().Info("Room created", zap.String("room_name", roomName), zap.String("connection_id", connectionID))
}

// HandleJoinRoom handles joining an existing LiveKit room
// POST /livekit/join-room
func (h *LiveKitHandler) HandleJoinRoom(w http.ResponseWriter, r *http.Request) {
	logger.Base().Info("🎙 Received join room request")

	var request JoinRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logger.Base().Error("Failed to decode request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if request.RoomName == "" {
		logger.Base().Error("Room name is required")
		http.Error(w, "Room name is required", http.StatusBadRequest)
		return
	}
	if request.ParticipantName == "" {
		logger.Base().Error("Participant name is required")
		http.Error(w, "Participant name is required", http.StatusBadRequest)
		return
	}

	if request.AgentID == "" {
		logger.Base().Error("Agent ID is required")
		http.Error(w, "Agent ID is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if request.VoiceLanguage == "" {
		request.VoiceLanguage = config.DefaultLanguage
	}

	// Validate agent ID and get TextAgentID if available
	var textAgentID string
	if h.agentService != nil {
		agentConfig, err := h.agentService.GetAgentConfigWithChannelType(context.Background(), request.AgentID, domain.ChannelTypeLiveKit)
		if err != nil || agentConfig == nil {
			logger.Base().Error("Agent validation failed")
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		// Use Agent ID from config (handles validation)
		request.AgentID = agentConfig.ID
		textAgentID = agentConfig.TextAgentID
		logger.Base().Info("Agent verified", zap.String("agent_id", request.AgentID))
	}

	logger.Base().Info("🎙 Joining existing room", zap.String("room_name", request.RoomName), zap.String("participant_name", request.ParticipantName), zap.String("agent_id", request.AgentID), zap.String("voice_language", request.VoiceLanguage))

	// Generate unique connection ID for this participant
	connectionID := fmt.Sprintf("livekit-%d", time.Now().UnixNano())

	// Create WhatsAppCallConnection (reusing existing structure)
	connection := &call.WhatsAppCallConnection{
		ID:            connectionID,
		CallID:        request.RoomName, // Use room name as call ID
		From:          request.ParticipantName,
		To:            fmt.Sprintf("%s%s", config.DefaultLiveKitBotPrefix, connectionID),
		CreatedAt:     time.Now(),
		LastActivity:  time.Now(),
		IsActive:      true,
		ChannelType:   domain.ChannelTypeLiveKit, // LiveKit channel doesn't need audio caching
		TenantID:      request.TenantID,
		AgentID:       request.AgentID,
		TextAgentID:   textAgentID,
		VoiceLanguage: request.VoiceLanguage,
	}

	// Register connection in service
	h.service.AddConnection(connection)

	// Initialize voice conversation for this connection
	if err := connection.InitializeVoiceConversation(); err != nil {
		logger.Base().Error("Failed to initialize voice conversation")
		// Continue anyway, AddMessage will create it as fallback
	}

	logger.Base().Info("Connection created", zap.String("connection_id", connectionID))

	// Generate LiveKit access token for the new participant
	accessToken, err := h.roomManager.GenerateToken(request.RoomName, request.ParticipantName)
	if err != nil {
		logger.Base().Error("Failed to generate token")
		h.service.CleanupConnection(connectionID)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Initialize AI model and join room in parallel (async)
	if h.taskBus != nil {
		logger.Base().Info("Enqueuing livekit-room task", zap.String("conn_id", connectionID))
		h.taskBus.Publish(context.Background(), task.SessionTask{
			Type:         task.TaskTypeLiveKitRoom,
			ConnectionID: connectionID,
			Payload:      nil,
		})
		// We still need to join the room locally as a bot
		go h.roomManager.JoinRoomAsBot(connectionID, request.RoomName, false)
	} else {
		// Fallback to local asynchronous processing
		go func() {
			// Enable greeting signal control (let Model handler wait for participant join before sending greeting)
			providerType := provider.ProviderTypeOpenAI
			if connection.ModelProvider == provider.ProviderTypeGemini {
				providerType = provider.ProviderTypeGemini
			}

			if modelHandler, err := h.service.GetModelHandler(providerType); err == nil && modelHandler != nil {
				modelHandler.EnableGreetingSignalControl(connectionID)
				logger.Base().Info("Enabled greeting signal control", zap.String("connection_id", connectionID), zap.String("provider", string(providerType)))
			}

			// Start both operations simultaneously
			go h.service.InitializeAIConnection(connection)
			h.roomManager.JoinRoomAsBot(connectionID, request.RoomName, false) // false = wait for signal control
		}()
	}

	logger.Base().Info("Participant joining room", zap.String("room_name", request.RoomName))

	// Get server URL from connection config
	serverURL := h.roomManager.GetConfigInternal().ServerURL

	// Return response with access token and server URL
	response := CreateRoomResponse{
		ConnectionID: connectionID,
		RoomName:     request.RoomName,
		AccessToken:  accessToken,
		ServerURL:    serverURL,
		Status:       "joined",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

	logger.Base().Info("Join response sent", zap.String("room_name", request.RoomName), zap.String("connection_id", connectionID))
}

// HandleEndCall handles ending a LiveKit call
// POST /livekit/end-call
func (h *LiveKitHandler) HandleEndCall(w http.ResponseWriter, r *http.Request) {
	logger.Base().Info("🎙 Received end call request")

	var request EndCallRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logger.Base().Error("Failed to decode request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if request.ConnectionID == "" {
		logger.Base().Error("Connection ID is required")
		http.Error(w, "Connection ID is required", http.StatusBadRequest)
		return
	}

	logger.Base().Info("🔚 Ending call for connection", zap.String("connection_id", request.ConnectionID))

	// Cleanup room and connection (via broadcast)
	h.roomManager.CleanupRoom(request.ConnectionID)
	h.service.NotifyCleanup(r.Context(), request.ConnectionID)

	// Return success response
	response := map[string]string{
		"status":  "success",
		"message": "Call ended successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

	logger.Base().Info("Call ended", zap.String("connection_id", request.ConnectionID))
}

// HandleConnectionStatus returns the status of a connection
// GET /livekit/connection-status/:connectionId
func (h *LiveKitHandler) HandleConnectionStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	connectionID := vars["connectionId"]

	if connectionID == "" {
		http.Error(w, "Connection ID is required", http.StatusBadRequest)
		return
	}

	logger.Base().Debug("Checking connection status", zap.String("connection_id", connectionID))

	// Get connection from service
	connection := h.service.GetConnection(connectionID)
	if connection == nil {
		logger.Base().Warn("Connection not found", zap.String("connection_id", connectionID))
		http.Error(w, "Connection not found", http.StatusNotFound)
		return
	}

	// Type assert to concrete type to access fields
	callConn, ok := connection.(*call.WhatsAppCallConnection)
	if !ok {
		http.Error(w, "Invalid connection type", http.StatusInternalServerError)
		return
	}

	// Build response
	response := ConnectionStatusResponse{
		ConnectionID:  callConn.ID,
		RoomName:      callConn.CallID, // CallID is room name
		ParticipantID: callConn.From,   // From is participant
		IsActive:      callConn.IsActive,
		IsAIReady:     callConn.GetAIWebRTC() != nil,
		AgentID:       callConn.GetAgentID(),
		CreatedAt:     callConn.CreatedAt,
		LastActivity:  callConn.LastActivity,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

	logger.Base().Info("Connection status retrieved", zap.String("connection_id", connectionID))
}

// HandleStats returns LiveKit statistics
// GET /livekit/stats
func (h *LiveKitHandler) HandleStats(w http.ResponseWriter, r *http.Request) {
	logger.Base().Info("📊 Retrieving stats")

	stats := map[string]interface{}{
		"activeRooms":       h.roomManager.GetRoomCount(),
		"activeConnections": h.service.GetConnectionCount(),
		"timestamp":         time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stats)

	logger.Base().Info("Stats retrieved")
}
