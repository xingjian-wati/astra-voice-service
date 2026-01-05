package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	httpadapter "github.com/ClareAI/astra-voice-service/internal/adapters/http"
	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/internal/core/task"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/internal/repository"
	"github.com/ClareAI/astra-voice-service/internal/services/agent"
	"github.com/ClareAI/astra-voice-service/internal/services/call"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// OutboundWebhookHandler handles webhook callbacks from Wati for outbound calls
type OutboundWebhookHandler struct {
	service      *call.WhatsAppCallService
	watiClient   *httpadapter.WatiClient
	repoManager  repository.RepositoryManager // Add repository manager for database operations
	agentService *agent.AgentService          // Add agent service for agent config retrieval
	taskBus      task.Bus                     // Task bus for asynchronous processing
}

// NewOutboundWebhookHandler creates a new outbound webhook handler
func NewOutboundWebhookHandler(service *call.WhatsAppCallService, watiClient *httpadapter.WatiClient, repoManager repository.RepositoryManager, taskBus task.Bus) *OutboundWebhookHandler {
	agentService, err := agent.GetAgentService()
	if err != nil {
		logger.Base().Error("Failed to get agent service")
	}

	return &OutboundWebhookHandler{
		service:      service,
		watiClient:   watiClient,
		repoManager:  repoManager,
		agentService: agentService,
		taskBus:      taskBus,
	}
}

// readRequestBody reads and logs the request body
func (h *OutboundWebhookHandler) readRequestBody(w http.ResponseWriter, r *http.Request, webhookType string) ([]byte, bool) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Base().Error("Failed to read request body", zap.String("webhooktype", webhookType))
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return nil, false
	}
	defer r.Body.Close()

	logger.Base().Info("webhook body", zap.String("body", string(bodyBytes)), zap.String("webhook_type", webhookType))
	return bodyBytes, true
}

// parseJSON parses JSON and handles errors
func (h *OutboundWebhookHandler) parseJSON(w http.ResponseWriter, bodyBytes []byte, target interface{}, webhookType string) bool {
	if err := json.Unmarshal(bodyBytes, target); err != nil {
		logger.Base().Error("Failed to parse webhook", zap.String("webhooktype", webhookType))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

// sendOKResponse sends a standard OK response
func (h *OutboundWebhookHandler) sendOKResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}

// PermissionWebhookRequest represents the permission webhook request from Wati
type PermissionWebhookRequest struct {
	WAID               string `json:"waid"`                         // WhatsApp user ID
	ChannelPhoneNumber string `json:"channelPhoneNumber,omitempty"` // Channel phone number (optional, for multi-channel support)
	HasPermission      bool   `json:"hasPermission"`                // Permission status
	Status             string `json:"status,omitempty"`             // Optional: "granted", "denied", etc.
	TenantID           string `json:"tenantId,omitempty"`           // Tenant ID (optional, for multi-tenant support)
}

// SDPAnswerWebhookRequest represents the SDP answer webhook request from Wati
type SDPAnswerWebhookRequest struct {
	CallID string `json:"callId"`
	SDP    string `json:"sdp"` // WebRTC answer SDP
}

// CallStatusWebhookRequest represents the call status webhook request from Wati
type CallStatusWebhookRequest struct {
	CallID string `json:"callId"`
	Status string `json:"status"` // "RINGING", "REJECTED", "ACCEPTED", "ENDED"
}

// NOTE: TerminateWebhookRequest has been removed - use wati_webhook_handler.go's
// handleTerminateWebhook at /wati/terminate for unified termination handling

// InitiateOutboundCallRequest represents the request to initiate an outbound call
type InitiateOutboundCallRequest struct {
	WAID               string `json:"waid"`                         // WhatsApp user ID (required)
	ChannelPhoneNumber string `json:"channelPhoneNumber,omitempty"` // Channel phone number (optional)
	AgentID            string `json:"agentId,omitempty"`            // Agent ID (optional)
	VoiceLanguage      string `json:"voiceLanguage,omitempty"`      // Voice language (optional, default: "en")
	Accent             string `json:"accent,omitempty"`             // Voice accent (optional)
	TenantID           string `json:"tenantId,omitempty"`           // Tenant ID (optional, will be cached for outbound calls)
}

// InitiateOutboundCallResponse represents the response from initiating an outbound call
type InitiateOutboundCallResponse struct {
	CallID       string `json:"callId"`       // WhatsApp call ID
	ConnectionID string `json:"connectionId"` // Internal connection ID
	Status       string `json:"status"`       // "initiated"
}

// SetupOutboundWebhookRoutes sets up routes for outbound call webhooks
func (h *OutboundWebhookHandler) SetupOutboundWebhookRoutes(router *mux.Router) {
	// Create webhook subrouter with CORS middleware
	webhookRouter := router.PathPrefix("/wati/outbound").Subrouter()
	webhookRouter.Use(CORSMiddleware)

	// POST /wati/outbound/initiate - Initiate outbound call (test mode, uses Draft config)
	webhookRouter.HandleFunc("/initiate", h.HandleInitiateOutboundCall).Methods("POST")

	// POST /wati/outbound/initiate-prod - Initiate outbound call (production mode, uses Published config)
	webhookRouter.HandleFunc("/initiate-prod", h.HandleInitiateOutboundCallProd).Methods("POST")

	// POST /wati/outbound/permission - Permission webhook
	webhookRouter.HandleFunc("/permission", h.HandlePermissionWebhook).Methods("POST")

	// POST /wati/outbound/sdp-answer - SDP answer webhook
	webhookRouter.HandleFunc("/sdp-answer", h.HandleSDPAnswerWebhook).Methods("POST")

	// POST /wati/outbound/call-status - Call status webhook
	webhookRouter.HandleFunc("/call-status", h.HandleCallStatusWebhook).Methods("POST")

	// NOTE: Terminate webhook is handled by wati_webhook_handler.go at /wati/terminate
	// This unified endpoint handles both inbound and outbound call termination

	logger.Base().Info("Outbound webhook routes registered")
}

// HandleInitiateOutboundCall handles initiating an outbound call (test mode)
// POST /wati/outbound/initiate
func (h *OutboundWebhookHandler) HandleInitiateOutboundCall(w http.ResponseWriter, r *http.Request) {
	h.handleInitiateOutboundCallInternal(w, r, domain.ChannelTypeTest)
}

// HandleInitiateOutboundCallProd handles initiating an outbound call (production mode)
// POST /wati/outbound/initiate-prod
func (h *OutboundWebhookHandler) HandleInitiateOutboundCallProd(w http.ResponseWriter, r *http.Request) {
	h.handleInitiateOutboundCallInternal(w, r, domain.ChannelTypeWhatsApp)
}

// handleInitiateOutboundCallInternal is the internal implementation for initiating outbound calls
func (h *OutboundWebhookHandler) handleInitiateOutboundCallInternal(w http.ResponseWriter, r *http.Request, channelType domain.ChannelType) {

	logger.Base().Info("Received initiate outbound call request (mode: )", zap.String("channel_type", string(channelType)))

	bodyBytes, ok := h.readRequestBody(w, r, "Initiate Outbound Call")
	if !ok {
		return
	}

	var request InitiateOutboundCallRequest
	if !h.parseJSON(w, bodyBytes, &request, "Initiate Outbound Call") {
		return
	}

	// Validate required fields
	if request.WAID == "" {
		logger.Base().Error("WAID is required")
		http.Error(w, "WAID is required", http.StatusBadRequest)
		return
	}

	// Set default voice language
	if request.VoiceLanguage == "" {
		request.VoiceLanguage = config.DefaultLanguage
	}

	// Usage gating: check tenant allowance before proceeding
	// Use tenant derived from agent (prefer agent's owner tenant over payload tenantId for billing)
	if h.agentService != nil && request.AgentID != "" {
		tenantID, err := h.agentService.GetTenantIDByAgentID(request.AgentID)
		if err != nil {
			logger.Base().Warn("[OutboundCall] Failed to resolve tenant for agent, using request tenantId", zap.String("agent_id", request.AgentID), zap.Error(err))
			tenantID = request.TenantID
		}

		if tenantID != "" && tenantID != config.DefaultTenantID && tenantID != config.DefaultWatiTenantID {
			if allowed, msg := h.agentService.CheckTenantUsageAllowed(context.Background(), tenantID); !allowed {
				logger.Base().Error("[OutboundCall] Usage not allowed for tenant", zap.String("tenant_id", tenantID), zap.String("agent_id", request.AgentID))
				http.Error(w, fmt.Sprintf("Usage not allowed: %s", msg), http.StatusForbidden)
				return
			}
		}
	}

	// Sanitize WAID and ChannelPhoneNumber by trimming non-digit characters from prefix and suffix
	sanitize := func(s string) string {
		return strings.TrimFunc(s, func(r rune) bool {
			return !unicode.IsDigit(r)
		})
	}

	request.WAID = sanitize(request.WAID)
	request.ChannelPhoneNumber = sanitize(request.ChannelPhoneNumber)

	logger.Base().Info("Initiating call to WAID: , channel: , agent: , language: , tenant", zap.String("waid", request.WAID), zap.String("channelphonenumber", request.ChannelPhoneNumber), zap.String("agent_id", request.AgentID), zap.String("voice_language", request.VoiceLanguage), zap.String("tenant_id", request.TenantID))

	// Step 1: Create a connection for this outbound call
	connection, err := h.createOutboundConnection(request, channelType)
	if err != nil {
		logger.Base().Error("Failed to create connection")
		http.Error(w, fmt.Sprintf("Failed to create connection: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Base().Info("Created connection: for WAID", zap.String("id", connection.ID), zap.String("waid", request.WAID))

	// Step 2: Check call permissions
	logger.Base().Debug("🔐 Checking call permissions for WAID", zap.String("waid", request.WAID))
	permissionResp, err := h.watiClient.GetCallPermissions(request.WAID, request.ChannelPhoneNumber, request.TenantID)
	if err != nil {
		logger.Base().Error("Failed to check permissions")
		h.service.CleanupConnection(connection.ID)
		http.Error(w, fmt.Sprintf("Failed to check permissions: %v", err), http.StatusInternalServerError)
		return
	}

	// Extract permission status from response
	// The data is nested under "result" field
	canStartCall := false
	canRequestPermission := false

	// First, extract the result object
	result, ok := permissionResp["result"].(map[string]interface{})
	if !ok {
		logger.Base().Error("Invalid response format: missing 'result' field")
		logger.Base().Info("Full permission response", zap.Any("permission_resp", permissionResp))
		h.service.CleanupConnection(connection.ID)
		http.Error(w, "Invalid permission response format", http.StatusInternalServerError)
		return
	}

	// Check both "start_call" and "send_call_permission_request" actions
	if actions, ok := result["actions"].([]interface{}); ok {
		for _, action := range actions {
			if actionMap, ok := action.(map[string]interface{}); ok {
				actionName, _ := actionMap["action_name"].(string)
				canPerform, _ := actionMap["can_perform_action"].(bool)

				switch actionName {
				case "start_call":
					canStartCall = canPerform
					logger.Base().Info("start_call permission", zap.Bool("can_perform", canPerform))
				case "send_call_permission_request":
					canRequestPermission = canPerform
					logger.Base().Info("send_call_permission_request permission", zap.Bool("can_perform", canPerform))
				}
			}
		}
	}

	if !canStartCall {
		logger.Base().Info("Permission status", zap.Bool("can_start_call", canStartCall), zap.Bool("can_request_permission", canRequestPermission))
	}

	// Step 3: Handle permission status
	if !canStartCall {
		// No permission to start call
		if !canRequestPermission {
			// Cannot request permission either - return error
			logger.Base().Error("Cannot start call and cannot request permission for WAID", zap.String("waid", request.WAID))
			h.service.CleanupConnection(connection.ID)
			http.Error(w, "Cannot start call: permission denied and cannot request permission (limit reached or not allowed)", http.StatusForbidden)
			return
		}

		// Can request permission - send permission request and wait for webhook
		logger.Base().Warn("No permission for WAID: , requesting permission", zap.String("waid", request.WAID))

		err := h.watiClient.SendCallPermissionRequest(request.WAID, request.ChannelPhoneNumber, request.TenantID)
		if err != nil {
			logger.Base().Error("Failed to request permission")
			h.service.CleanupConnection(connection.ID)
			http.Error(w, fmt.Sprintf("Failed to request permission: %v", err), http.StatusInternalServerError)
			return
		}

		// Store a temporary message ID for tracking (will be updated by webhook)
		connection.PermissionMessageID = fmt.Sprintf("perm-req-%d", time.Now().UnixNano())
		logger.Base().Info("Permission request sent, waiting for webhook")

		// Return response indicating waiting for permission
		responseData := InitiateOutboundCallResponse{
			CallID:       "", // No call ID yet
			ConnectionID: connection.ID,
			Status:       "waiting_permission",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(responseData)

		logger.Base().Info("Waiting for permission webhook for connection", zap.String("id", connection.ID))
		return
	}

	// Step 4: Has permission - proceed to make call
	logger.Base().Info("Permission granted for WAID: , proceeding with call", zap.String("waid", request.WAID))
	err = h.proceedWithOutboundCall(connection, request.TenantID)
	if err != nil {
		logger.Base().Error("Failed to proceed with call")
		h.service.CleanupConnection(connection.ID)
		http.Error(w, fmt.Sprintf("Failed to make call: %v", err), http.StatusInternalServerError)
		return
	}

	// Return response (ai model will be initialized when phone starts ringing)
	responseData := InitiateOutboundCallResponse{
		CallID:       connection.CallID,
		ConnectionID: connection.ID,
		Status:       "calling",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(responseData)

	logger.Base().Info("Call initiated, ai will be ready when user answers")
}

// createOutboundConnection creates a new connection for outbound call
func (h *OutboundWebhookHandler) createOutboundConnection(request InitiateOutboundCallRequest, channelType domain.ChannelType) (*call.WhatsAppCallConnection, error) {
	connectionID := fmt.Sprintf("outbound-%d", time.Now().UnixNano())

	connection := &call.WhatsAppCallConnection{
		ID:             connectionID,
		From:           request.WAID,
		To:             request.ChannelPhoneNumber,
		IsActive:       true,
		IsOutboundCall: true,        // Mark as outbound call for signal-controlled greeting
		ChannelType:    channelType, // WhatsApp channel needs audio caching
		VoiceLanguage:  request.VoiceLanguage,
		Accent:         request.Accent,
		CallID:         "", // Will be set after MakeOutboundCall
		TenantID:       request.TenantID,
		CreatedAt:      time.Now(),
		LastActivity:   time.Now(),
		RepoManager:    h.repoManager, // Add repository manager for database operations
		ContactName:    request.WAID,
	}

	// Set agent ID if provided
	if request.AgentID != "" {
		connection.SetAgentID(request.AgentID)
		if h.agentService != nil {
			// Use connection's channelType to get appropriate config (Draft for test, Published for production)
			agentConfig, err := h.agentService.GetAgentConfigWithChannelType(context.Background(), request.AgentID, channelType)
			if err != nil {
				logger.Base().Error("Failed to get agent config for ID", zap.String("agent_id", request.AgentID))
			} else {
				connection.SetAgentID(agentConfig.ID)
				connection.SetTextAgentID(agentConfig.TextAgentID)
			}
		}
	}
	// Add connection to service
	h.service.AddConnection(connection)

	return connection, nil
}

// proceedWithOutboundCall generates SDP offer and makes the outbound call
func (h *OutboundWebhookHandler) proceedWithOutboundCall(connection *call.WhatsAppCallConnection, tenantID string) error {
	// Generate SDP offer
	webrtcProcessor := h.service.GetWebRTCProcessor()
	sdpOffer, err := webrtcProcessor.GenerateSDPOffer(connection.ID)
	if err != nil {
		return fmt.Errorf("failed to create SDP offer: %v", err)
	}

	logger.Base().Info("Generated SDP offer for connection", zap.String("connection_id", connection.ID), zap.Int("length", len(sdpOffer)))

	// Call Wati API to make outbound call
	logger.Base().Info("Calling Wati API for connection ...", zap.String("id", connection.ID))
	startTime := time.Now()
	response, err := h.watiClient.MakeOutboundCall(connection.From, sdpOffer, connection.To, "", tenantID)
	if err != nil {
		return fmt.Errorf("failed to make outbound call: %v", err)
	}
	logger.Base().Info("Wati API responded", zap.Duration("duration", time.Since(startTime)), zap.String("call_id", response.Result.CallID))

	// Check if another connection with the same CallID already exists
	existingConn, existingID := h.service.GetConnectionByCallID(response.Result.CallID)
	if existingConn != nil {
		logger.Base().Warn("Found existing connection with same CallID, cleaning up old connection", zap.String("call_id", response.Result.CallID), zap.String("existing_connection_id", existingID))
		h.service.CleanupConnection(existingID)
	}

	// Store the call ID in the connection (use Mutex for thread-safety)
	connection.Mutex.Lock()
	connection.CallID = response.Result.CallID
	connection.Mutex.Unlock()
	logger.Base().Info("Call initiated successfully", zap.String("call_id", response.Result.CallID), zap.String("connection_id", connection.ID))

	return nil
}

// findPendingConnectionByWAID finds a pending outbound connection by WAID and optionally by channel
func (h *OutboundWebhookHandler) findPendingConnectionByWAID(waid, channelPhoneNumber string) (*call.WhatsAppCallConnection, string) {
	connections := h.service.GetAllConnections()
	for id, conn := range connections {
		// Find outbound connection that's waiting for permission
		// Match by WAID and optionally by channelPhoneNumber (for multi-channel support)
		if conn.From == waid &&
			(channelPhoneNumber == "" || conn.To == channelPhoneNumber) &&
			conn.CallID == "" &&
			conn.PermissionMessageID != "" {
			return conn, id
		}
	}
	return nil, ""
}

// HandlePermissionWebhook handles permission webhook from Wati
// POST /wati/outbound/permission
func (h *OutboundWebhookHandler) HandlePermissionWebhook(w http.ResponseWriter, r *http.Request) {
	logger.Base().Info("Received permission webhook")

	bodyBytes, ok := h.readRequestBody(w, r, "Permission")
	if !ok {
		return
	}

	var request PermissionWebhookRequest
	if !h.parseJSON(w, bodyBytes, &request, "Permission") {
		return
	}

	logger.Base().Info("Permission status", zap.String("waid", request.WAID), zap.String("channel_phone_number", request.ChannelPhoneNumber), zap.Bool("has_permission", request.HasPermission), zap.String("status", request.Status))

	// Find pending connection by WAID and optionally by channel
	connection, connectionID := h.findPendingConnectionByWAID(request.WAID, request.ChannelPhoneNumber)
	if connection == nil {
		logger.Base().Warn("No pending connection found for WAID", zap.String("waid", request.WAID))
		http.Error(w, "Connection not found", http.StatusNotFound)
		return
	}

	logger.Base().Info("Found pending connection: for WAID", zap.String("waid", request.WAID))

	// Handle permission status
	if !request.HasPermission {
		// Permission denied - cleanup connection
		logger.Base().Error("Permission denied for connection: , WAID", zap.String("from", connection.From))
		h.service.CleanupConnection(connectionID)
		h.sendOKResponse(w)
		return
	}

	// Permission granted - proceed with outbound call
	logger.Base().Info("Permission granted for connection: , WAID", zap.String("from", connection.From))

	// Use tenant ID from connection (stored during initiation) or from webhook request
	tenantID := connection.TenantID
	if tenantID == "" && request.TenantID != "" {
		tenantID = request.TenantID
		connection.SetTenantID(tenantID)
	}

	err := h.proceedWithOutboundCall(connection, tenantID)
	if err != nil {
		logger.Base().Error("Failed to proceed with call after permission granted")
		h.service.CleanupConnection(connectionID)
		http.Error(w, fmt.Sprintf("Failed to make call: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Base().Info("Call initiated after permission granted, CallID: , waiting for SDP answer", zap.String("call_id", connection.CallID))
	h.sendOKResponse(w)
}

// HandleSDPAnswerWebhook handles SDP answer webhook from Wati
// POST /wati/outbound/sdp-answer
func (h *OutboundWebhookHandler) HandleSDPAnswerWebhook(w http.ResponseWriter, r *http.Request) {
	logger.Base().Info("Received SDP answer webhook")

	bodyBytes, ok := h.readRequestBody(w, r, "SDP Answer")
	if !ok {
		return
	}

	var request SDPAnswerWebhookRequest
	if !h.parseJSON(w, bodyBytes, &request, "SDP Answer") {
		return
	}

	logger.Base().Info("SDP Answer received", zap.String("call_id", request.CallID), zap.Int("sdp_length", len(request.SDP)))

	// Find the connection by CallID
	connection, connectionID := h.service.GetConnectionByCallID(request.CallID)
	if connection == nil {
		logger.Base().Warn("Connection not found for callId", zap.String("call_id", request.CallID))

		// Debug: List all active connections
		allConnections := h.service.GetAllConnections()
		logger.Base().Debug("Active connections", zap.Int("count", len(allConnections)))
		for id, conn := range allConnections {
			conn.Mutex.RLock()
			logger.Base().Debug("Connection details", zap.String("id", id), zap.String("call_id", conn.CallID), zap.String("from", conn.From), zap.String("to", conn.To), zap.Duration("age", time.Since(conn.CreatedAt)))
			conn.Mutex.RUnlock()
		}

		http.Error(w, "Connection not found", http.StatusNotFound)
		return
	}

	logger.Base().Info("Found connection: for callId", zap.String("call_id", request.CallID))

	// Apply SDP answer to WebRTC connection
	logger.Base().Info("Processing SDP answer for connection", zap.String("connection_id", connectionID))

	if !connection.IsAIReady {
		connection.HasInboundAudio = true
		h.service.InitializeAIConnection(connection)
		logger.Base().Info("AI ready! Waiting for user to accept...")
	}

	webrtcProcessor := h.service.GetWebRTCProcessor()
	err := webrtcProcessor.ProcessSDPAnswer(connectionID, request.SDP)
	if err != nil {
		logger.Base().Error("Failed to process SDP answer")
		http.Error(w, "Failed to process SDP answer", http.StatusInternalServerError)
		return
	}

	logger.Base().Info("Successfully established WebRTC connection", zap.String("connection_id", connectionID))
	logger.Base().Info("WebRTC ready for CallID: , waiting for user to accept call", zap.String("call_id", request.CallID))
	logger.Base().Info("AI will be initialized when call is accepted")

	h.sendOKResponse(w)
}

// HandleCallStatusWebhook handles call status webhook from Wati
// POST /wati/outbound/call-status
func (h *OutboundWebhookHandler) HandleCallStatusWebhook(w http.ResponseWriter, r *http.Request) {
	logger.Base().Info("Received call status webhook")

	bodyBytes, ok := h.readRequestBody(w, r, "Call Status")
	if !ok {
		return
	}

	var request CallStatusWebhookRequest
	if !h.parseJSON(w, bodyBytes, &request, "Call Status") {
		return
	}

	logger.Base().Info("Call status: callId=, status=", zap.String("call_id", request.CallID), zap.String("status", request.Status))

	// Handle different call statuses
	h.handleCallStatus(request.CallID, request.Status)

	h.sendOKResponse(w)
}

// handleCallStatus handles different call status types
func (h *OutboundWebhookHandler) handleCallStatus(callID, status string) {
	connection, _ := h.service.GetConnectionByCallID(callID)

	switch status {
	case "RINGING":
		logger.Base().Info("Call is ringing", zap.String("call_id", callID))

		if connection == nil {
			logger.Base().Warn("Connection not found for CallID", zap.String("call_id", callID))
			return
		}

		// Pre-initialize AI while phone is ringing (optimization)
		// When user accepts, AI will be ready for immediate greeting
		logger.Base().Info("🚀 Phone ringing, pre-initializing AI for instant response...")

	case "ACCEPTED":
		logger.Base().Info("Call was accepted by user", zap.String("call_id", callID))

		if connection == nil {
			logger.Base().Warn("Connection not found for CallID", zap.String("call_id", callID))
			return
		}

		// Initialize voice conversation now that CallID is set
		if err := connection.InitializeVoiceConversation(); err != nil {
			logger.Base().Error("Failed to initialize voice conversation")
			// Continue anyway, AddMessage will create it as fallback
		}
		// Mark as audio active (consistent with inbound calls)
		logger.Base().Info("Audio processing enabled for CallID", zap.String("call_id", callID))

		// Handle session initialization via Task Bus
		if h.taskBus != nil {
			logger.Base().Info("Enqueuing outbound-call task", zap.String("conn_id", connection.ID))
			h.taskBus.Publish(context.Background(), task.SessionTask{
				Type:         task.TaskTypeOutboundCall,
				ConnectionID: connection.ID,
				Payload:      nil, // No specific payload needed for outbound setup
			})
		} else {
			// Fallback to local asynchronous processing
			if connection.IsOutboundCall {
				go h.waitAndTriggerGreeting(connection.ID, callID)
			}
		}

		logger.Base().Info("Audio forwarding will start automatically via event system")
		logger.Base().Info("Call is now fully active for CallID", zap.String("call_id", callID))

	case "REJECTED":
		logger.Base().Error("Call was rejected by user", zap.String("call_id", callID))
		logger.Base().Info("AI was not initialized (avoided resource waste)")
		h.service.NotifyCleanupByCallID(context.Background(), callID)

	case "ENDED":
		logger.Base().Info("🔚 Call has ended", zap.String("call_id", callID))
		h.service.NotifyCleanupByCallID(context.Background(), callID)

	default:
		logger.Base().Warn("Unknown call status: for CallID", zap.String("call_id", callID), zap.String("status", status))
	}
}

// waitAndTriggerGreeting waits for AI to be ready and triggers greeting signal
func (h *OutboundWebhookHandler) waitAndTriggerGreeting(connectionID, callID string) {
	logger.Base().Info("Waiting for AI ready", zap.String("call_id", callID))

	const maxWaitTime = 30 * time.Second
	const checkInterval = 100 * time.Millisecond
	startTime := time.Now()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if connection still exists
			connection := h.service.GetConnection(connectionID)
			if connection == nil {
				logger.Base().Warn("Connection no longer exists", zap.String("connection_id", connectionID))
				return
			}

			// Check if AI is ready
			if connection.GetIsAIReady() {
				logger.Base().Info("AI ready after, triggering greeting", zap.Duration("duration", time.Since(startTime)), zap.String("call_id", callID))

				// Determine provider type
				providerType := provider.ProviderTypeOpenAI
				if callConn, ok := connection.(*call.WhatsAppCallConnection); ok {
					if callConn.ModelProvider == provider.ProviderTypeGemini {
						providerType = provider.ProviderTypeGemini
					}
				}

				// Trigger greeting signal
				if modelHandler, err := h.service.GetModelHandler(providerType); err == nil && modelHandler != nil {
					modelHandler.TriggerGreeting(connectionID)
				}
				return
			}

			// Check timeout
			if time.Since(startTime) >= maxWaitTime {
				logger.Base().Info("⏰ AI ready timeout (10s), broadcasting cleanup", zap.String("call_id", callID))
				h.service.NotifyCleanupByCallID(context.Background(), callID)
				return
			}

		case <-time.After(maxWaitTime):
			logger.Base().Info("⏰ AI ready timeout (10s), broadcasting cleanup", zap.String("call_id", callID))
			h.service.NotifyCleanupByCallID(context.Background(), callID)
			return
		}
	}
}

// NOTE: HandleTerminateWebhook has been removed and consolidated with
// wati_webhook_handler.go's handleTerminateWebhook at /wati/terminate
// This unified endpoint handles both inbound and outbound call termination
