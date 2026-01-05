package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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

// WatiWebhookHandler handles HTTP requests for Wati webhooks
type WatiWebhookHandler struct {
	service           *call.WhatsAppCallService
	watiClient        *httpadapter.WatiClient
	processedWebhooks map[string]time.Time // Track processed webhook IDs to prevent duplicates
	webhookSecret     string               // Secret key for webhook verification
	agentID           string               // Agent ID for this handler
	agentService      *agent.AgentService
	tenantIDRequired  map[string]bool              // Map of endpoints that require tenant_id validation
	repoManager       repository.RepositoryManager // Repository manager for database operations
	taskBus           task.Bus                     // Task bus for asynchronous session initialization
}

// NewWatiWebhookHandler creates a new Wati webhook handler
func NewWatiWebhookHandler(service *call.WhatsAppCallService, watiClient *httpadapter.WatiClient, webhookSecret string, agentID string, repoManager repository.RepositoryManager, taskBus task.Bus) *WatiWebhookHandler {
	agentService, err := agent.GetAgentService()
	if err != nil {
		logger.Base().Error("Failed to get agent service", zap.Error(err))
		return nil
	}

	// Configure which endpoints require tenant_id validation
	tenantIDRequired := map[string]bool{
		"new-call":          true,
		"terminate-call":    true,
		"web-new-call":      true,
		"calls/*/accept":    true,
		"calls/*/terminate": true,
	}

	return &WatiWebhookHandler{
		service:           service,
		watiClient:        watiClient,
		processedWebhooks: make(map[string]time.Time),
		webhookSecret:     webhookSecret,
		agentID:           agentID,
		agentService:      agentService,
		tenantIDRequired:  tenantIDRequired,
		repoManager:       repoManager,
		taskBus:           taskBus,
	}
}

// SetTenantIDValidation configures which endpoints require tenant_id validation
func (h *WatiWebhookHandler) SetTenantIDValidation(endpoint string, required bool) {
	if h.tenantIDRequired == nil {
		h.tenantIDRequired = make(map[string]bool)
	}
	h.tenantIDRequired[endpoint] = required
	logger.Base().Info("TenantIDValidation: Set validation for endpoint", zap.String("endpoint", endpoint), zap.Bool("required", required))
}

// GetTenantIDValidationConfig returns the current validation configuration
func (h *WatiWebhookHandler) GetTenantIDValidationConfig() map[string]bool {
	return h.tenantIDRequired
}

// tenantIDValidationMiddleware validates that tenant_id is present in required endpoints
func (h *WatiWebhookHandler) tenantIDValidationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only validate POST requests that need tenant_id
		if r.Method == "POST" && h.shouldValidateTenantID(r.URL.Path) {
			if !h.validateTenantID(w, r) {
				return // Validation failed, response already sent
			}
		}

		next.ServeHTTP(w, r)
	})
}

// shouldValidateTenantID determines if the request should be validated for tenant_id
func (h *WatiWebhookHandler) shouldValidateTenantID(fullPath string) bool {
	// Remove /wati prefix to get the relative path
	path := strings.TrimPrefix(fullPath, "/wati/")
	path = strings.TrimLeft(path, "/")

	logger.Base().Debug("TenantIDValidation: Checking path", zap.String("path", path))

	// Check exact matches first
	if required, exists := h.tenantIDRequired[path]; exists && required {
		logger.Base().Debug("TenantIDValidation: Exact match found for path", zap.String("path", path))
		return true
	}

	// Check pattern matches for dynamic routes like calls/{id}/accept
	for pattern, required := range h.tenantIDRequired {
		if required && strings.Contains(pattern, "*") {
			// Convert pattern to regex-like matching
			if h.matchesPattern(path, pattern) {
				logger.Base().Debug("TenantIDValidation: Pattern match found", zap.String("path", path), zap.String("pattern", pattern))
				return true
			}
		}
	}

	logger.Base().Debug("TenantIDValidation: No validation required for path", zap.String("path", path))
	return false
}

// matchesPattern checks if a path matches a pattern with wildcards
func (h *WatiWebhookHandler) matchesPattern(path, pattern string) bool {
	pathParts := strings.Split(path, "/")
	patternParts := strings.Split(pattern, "/")

	if len(pathParts) != len(patternParts) {
		return false
	}

	for i, patternPart := range patternParts {
		if patternPart != "*" && patternPart != pathParts[i] {
			return false
		}
	}

	return true
}

// validateTenantID validates that tenant_id is present in the request body
func (h *WatiWebhookHandler) validateTenantID(w http.ResponseWriter, r *http.Request) bool {
	logger.Base().Debug("TenantIDValidation: Validating tenant_id", zap.String("method", r.Method), zap.String("path", r.URL.Path))

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendTenantIDError(w, "Failed to read request body")
		return false
	}

	// Restore body for next handler
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	// Check if body is empty
	if len(body) == 0 {
		h.sendTenantIDError(w, "Request body is required")
		return false
	}

	// Parse JSON body
	var jsonBody map[string]interface{}
	if err := json.Unmarshal(body, &jsonBody); err != nil {
		h.sendTenantIDError(w, "Invalid JSON format")
		return false
	}

	// Check if tenant_id exists and is not empty
	tenantID, exists := jsonBody["tenantId"]
	if !exists {
		h.sendTenantIDError(w, "Field 'tenantId' is required")
		return false
	}

	// Check if tenant_id is a string and not empty
	tenantIDStr, ok := tenantID.(string)
	if !ok {
		h.sendTenantIDError(w, "Field 'tenantId' must be a string")
		return false
	}

	if strings.TrimSpace(tenantIDStr) == "" {
		h.sendTenantIDError(w, "Field 'tenantId' cannot be empty")
		return false
	}
	if !h.agentService.TenantExists(tenantIDStr) {
		h.sendTenantIDError(w, "Tenant not found")
		return false
	}
	logger.Base().Info("TenantIDValidation: tenant_id validation passed", zap.String("tenant_id", tenantIDStr))
	return true
}

// sendTenantIDError sends a tenant_id validation error response
func (h *WatiWebhookHandler) sendTenantIDError(w http.ResponseWriter, message string) {
	logger.Base().Error("TenantIDValidation error", zap.String("message", message))

	response := map[string]interface{}{
		"success": false,
		"message": message,
		"code":    "TENANT_ID_REQUIRED",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(response)
}

// SetupWatiRoutes sets up routes for Wati webhooks with tenant_id validation middleware
func (h *WatiWebhookHandler) SetupWatiRoutes(router *mux.Router) {
	// Create a subrouter for Wati routes with tenant_id validation middleware
	watiRouter := router.PathPrefix("/wati").Subrouter()

	// Apply tenant_id validation middleware to Wati routes
	watiRouter.Use(h.tenantIDValidationMiddleware)

	logger.Base().Info("WatiRoutes: Applied tenant_id validation middleware to Wati routes")
	logger.Base().Info("WatiRoutes: Validation required for endpoints", zap.Any("endpoints", h.tenantIDRequired))

	// Wati webhook endpoint (for receiving webhooks from Wati)
	watiRouter.HandleFunc("/webhook", h.handleWatiWebhook).Methods("POST")

	// Wati API endpoints (for Wati to call your backend)
	watiRouter.HandleFunc("/new-call", h.handleNewCall).Methods("POST")
	watiRouter.HandleFunc("/web-new-call", h.handleWebCallEndpoint).Methods("POST")
	watiRouter.HandleFunc("/terminate-call", h.handleTerminateCall).Methods("POST")

	// Legacy endpoints (for backward compatibility)
	watiRouter.HandleFunc("/terminate", h.handleTerminateWebhook).Methods("POST")

	// Manual call control endpoints (for testing)
	// Note: These endpoints require tenantId in the request body since it's not in the URL
	watiRouter.HandleFunc("/calls/{callId}/accept", h.handleManualAccept).Methods("POST")
	watiRouter.HandleFunc("/calls/{callId}/terminate", h.handleManualTerminate).Methods("POST")

	// Test mode endpoints (no Wati client dependencies, no tenant validation)
	watiRouter.HandleFunc("/test/new-call", h.handleTestNewCall).Methods("POST")
	watiRouter.HandleFunc("/test/terminate-call", h.handleTestTerminateCall).Methods("POST")

	// Status and health endpoints (no validation needed for these)
	watiRouter.HandleFunc("/status", h.handleStatus).Methods("GET")
	watiRouter.HandleFunc("/health", h.handleHealth).Methods("GET")

	logger.Base().Info("WatiRoutes: All Wati webhook routes registered with tenant_id validation")
}

// verifyWebhookSignature verifies the webhook signature using HMAC-SHA256
func (h *WatiWebhookHandler) verifyWebhookSignature(payload []byte, signature string) bool {
	logger.Base().Debug("Verifying webhook signature")
	logger.Base().Debug("Secret configured", zap.Bool("configured", h.webhookSecret != ""))
	logger.Base().Debug("Secret length", zap.Int("length", len(h.webhookSecret)))
	logger.Base().Debug("Signature provided", zap.String("signature", signature))

	if h.webhookSecret == "" {
		logger.Base().Warn("No webhook secret configured, skipping signature verification")
		return true // Allow if no secret is configured
	}

	// Remove "sha256=" prefix if present
	signature = strings.TrimPrefix(signature, "sha256=")

	// Create HMAC hash
	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	// Compare signatures using constant time comparison
	result := hmac.Equal([]byte(signature), []byte(expectedSignature))
	logger.Base().Debug("Expected signature", zap.String("expected", expectedSignature))
	logger.Base().Debug("Provided signature", zap.String("provided", signature))
	logger.Base().Debug("Verification result", zap.Bool("result", result))
	return result
}

// handleWatiWebhook godoc
// @Summary Handle Wati webhook
// @Description Receive and process webhook events from Wati (incoming call notifications)
// @Tags wati
// @Accept json
// @Produce json
// @Param X-Hub-Signature-256 header string false "Webhook signature for verification"
// @Param webhook body object true "Wati webhook event payload"
// @Success 200 {string} string "OK"
// @Failure 400 {string} string "Bad Request"
// @Failure 401 {string} string "Unauthorized (invalid signature)"
// @Router /wati/webhook [post]
func (h *WatiWebhookHandler) handleWatiWebhook(w http.ResponseWriter, r *http.Request) {
	h.logWebhookRequest(r)

	// Read and validate request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Base().Error("Failed to read Wati webhook body: %v", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	h.logWebhookBody(body)

	// Verify webhook signature
	if !h.validateWebhookSignature(r, body) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse and process webhook event (API Key extraction happens inside)
	event, err := h.parseWebhookEvent(body, r)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Check for duplicate processing
	if h.isDuplicateWebhook(event) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}

	// Process the webhook event
	h.processWebhookEvent(event, body)

	// Respond with success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleCallStart handles call start events from Wati
func (h *WatiWebhookHandler) handleCallStart(event httpadapter.WatiWebhookEvent, body []byte) {
	logger.Base().Info("Call starting", zap.String("call_id", event.CallID))

	// Extract phone number and contact name from contact field only
	var phoneNumber, contactName string

	if event.TenantID != "" && h.agentService != nil {
		// Use tenant derived from event, but skip usage check for default tenants
		if event.TenantID != config.DefaultTenantID && event.TenantID != config.DefaultWatiTenantID {
			if allowed, msg := h.agentService.CheckTenantUsageAllowed(context.Background(), event.TenantID); !allowed {
				logger.Base().Error("Usage not allowed for tenant", zap.String("tenant_id", event.TenantID), zap.String("message", msg))
				return
			}
		}
	}
	// Only use contact information from webhook
	if event.Contact != nil && event.Contact.ContactNumber != "" {
		phoneNumber = event.Contact.ContactNumber
		contactName = event.Contact.ContactName
		logger.Base().Info("Using contact info", zap.String("phone_number", phoneNumber), zap.String("contact_name", contactName))
	} else {
		logger.Base().Error("No contact information available in webhook", zap.Any("contact", event.Contact))
		logger.Base().Error("Cannot process call without contact information")
		return // Abort processing - no contact info means we can't handle this call
	}

	logger.Base().Info("DUPLICATE PREVENTION DISABLED: Proceeding with new connection", zap.String("phone_number", phoneNumber))
	// Select agent based on businessNumber field
	selectedAgentID := h.agentID
	var textAgentID string
	var agentCfg *config.AgentConfig
	var err error

	if event.AgentMapping != nil {
		if event.AgentMapping.AgentID != "" {
			// Use empty channelType to get Published config (default behavior for webhook)
			agentCfg, err = h.agentService.GetAgentConfigWithChannelType(context.Background(), event.AgentMapping.AgentID, "")
			if err != nil {
				logger.Base().Error("Failed to get agent config by agent ID: %v", zap.Error(err))
			} else if agentCfg != nil {
				selectedAgentID = agentCfg.ID
				textAgentID = agentCfg.TextAgentID
			}
		} else if event.AgentMapping.TenantID != "" {
			agents, err := h.agentService.GetAgentConfigByTenantID(context.Background(), event.AgentMapping.TenantID)
			if err != nil {
				logger.Base().Error("Failed to get agent config by tenant ID: %v", zap.Error(err))
				return
			}
			for _, agent := range agents {
				selectedAgentID = agent.ID
				agentCfg = agent
				if agent.BusinessNumber == event.BusinessNumber {
					break
				}
			}
			// Update textAgentID from the selected agent
			if agentCfg != nil {
				textAgentID = agentCfg.TextAgentID
			}
		}
	}

	// Use language from Wati webhook if provided, otherwise default to English
	var voiceLanguage, countryCode string
	if event.VoiceLanguage != "" {
		voiceLanguage = event.VoiceLanguage
		countryCode = "US" // Default country code
		logger.Base().Info("Using voice language from Wati: %s (country: %s)", zap.String("voice_language", voiceLanguage), zap.String("country_code", countryCode))
	} else {
		// Fallback to English
		if agentCfg != nil {
			voiceLanguage = agentCfg.Language
		} else {
			voiceLanguage = "en"
			logger.Base().Info("⚠️ Agent config not found, defaulting to language: %s", zap.String("value", voiceLanguage))
		}
		countryCode = "US"
		logger.Base().Info("Using default language: %s (country: %s)", zap.String("voice_language", voiceLanguage), zap.String("country_code", countryCode))
	}

	// Create new connection for this call
	connectionID := fmt.Sprintf("wati_%s_%d", event.CallID, time.Now().UnixNano())
	connection := &call.WhatsAppCallConnection{
		ID:             connectionID,
		CallID:         event.CallID,
		From:           phoneNumber, // Use extracted phone number
		To:             "",          // Not provided in webhook
		CreatedAt:      time.Now(),
		LastActivity:   time.Now(),
		IsActive:       true,
		ChannelType:    domain.ChannelTypeWhatsApp, // WhatsApp channel needs audio caching
		StopKeepalive:  make(chan struct{}),
		VoiceLanguage:  voiceLanguage,
		CountryCode:    countryCode,
		ContactName:    contactName,          // Set contact name from webhook
		AgentID:        selectedAgentID,      // Set selected agent ID
		TextAgentID:    textAgentID,          // Set text agent ID
		TenantID:       event.TenantID,       // Set tenant ID from webhook
		BusinessNumber: event.BusinessNumber, // Set business number from webhook
		RepoManager:    h.repoManager,        // Add repository manager for database operations
	}

	// Store connection
	h.service.AddConnection(connection)

	// Initialize voice conversation for this connection
	if err := connection.InitializeVoiceConversation(); err != nil {
		logger.Base().Warn("Failed to initialize voice conversation: %v", zap.Error(err))
		// Continue anyway, AddMessage will create it as fallback
	}

	// Handle SDP offer and AI initialization via Task Bus (distributed asynchronous)
	if h.taskBus != nil {
		logger.Base().Info("Enqueuing inbound call task", zap.String("conn_id", connectionID))
		h.taskBus.Publish(context.Background(), task.SessionTask{
			Type:         task.TaskTypeInboundCall,
			ConnectionID: connectionID,
			Payload:      body, // Reusing the raw webhook body for worker processing
		})
	} else {
		// Fallback to local asynchronous processing
		sdpData, err := event.ParseSDP()
		if err == nil && sdpData != nil && sdpData.Type == "offer" {
			go h.processCallOffer(event.TenantID, event.CallID, sdpData.SDP, connection)
		}
		go h.service.InitializeAIConnection(connection)
	}

	logger.Base().Info("✅ Wati call connection created: %s", zap.String("value", connectionID))
}

// handleTerminateWebhook godoc
// @Summary Handle terminate webhook (legacy)
// @Description Legacy endpoint for handling call termination webhooks from Wati
// @Tags wati
// @Accept json
// @Produce json
// @Param X-Hub-Signature-256 header string false "Webhook signature for verification"
// @Param terminate body object true "Terminate webhook payload"
// @Success 200 {string} string "OK"
// @Failure 400 {string} string "Bad Request"
// @Failure 401 {string} string "Unauthorized (invalid signature)"
// @Router /wati/terminate [post]
func (h *WatiWebhookHandler) handleTerminateWebhook(w http.ResponseWriter, r *http.Request) {
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Base().Error("Failed to read terminate webhook body: %v", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	logger.Base().Info("Terminate webhook received", zap.String("body", string(body)))

	// Verify webhook signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if signature != "" {
		if !h.verifyWebhookSignature(body, signature) {
			logger.Base().Error("Invalid terminate webhook signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		logger.Base().Info("Terminate webhook signature verified")
	} else {
		logger.Base().Warn("No terminate webhook signature provided")
	}

	// Parse and process webhook event (API Key extraction happens inside)
	event, err := h.parseWebhookEvent(body, r)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	logger.Base().Info("Terminate webhook for call", zap.String("call_id", event.CallID), zap.String("tenant_id", event.TenantID))

	// Check for duplicate webhook processing
	webhookKey := fmt.Sprintf("terminate_%s_%s", event.CallID, event.TenantID)
	if processedTime, exists := h.processedWebhooks[webhookKey]; exists {
		// If processed within last 30 seconds, consider it a duplicate
		if time.Since(processedTime) < 30*time.Second {
			logger.Base().Warn("Duplicate terminate webhook detected, ignoring", zap.String("webhook_key", webhookKey), zap.Duration("processed_ago", time.Since(processedTime)))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
			return
		}
	}

	// Mark webhook as processed
	h.processedWebhooks[webhookKey] = time.Now()

	// Process the terminate event
	logger.Base().Info("Call ending", zap.String("call_id", event.CallID))

	// Find and cleanup connection (via broadcast)
	if err := h.service.NotifyCleanupByCallID(r.Context(), event.CallID); err != nil {
		logger.Base().Error("Failed to broadcast terminate task", zap.Error(err))
	} else {
		logger.Base().Info("Broadcast cleanup for ended call", zap.String("call_id", event.CallID))
	}

	// Respond with 200 OK immediately
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleNewCall godoc
// @Summary Create new call
// @Description Accept a new incoming call from Wati with SDP offer
// @Tags wati
// @Accept json
// @Produce json
// @Param call body object true "New call request with tenantId, callId, and SDP offer"
// @Success 200 {object} object "Call accepted successfully"
// @Failure 400 {object} object "Bad request - missing required fields"
// @Failure 500 {object} object "Internal server error"
// @Router /wati/new-call [post]
func (h *WatiWebhookHandler) handleNewCall(w http.ResponseWriter, r *http.Request) {
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Base().Error("Failed to read new call request body: %v", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	logger.Base().Info("New call request received", zap.String("body", string(body)))

	// Parse request payload
	var request httpadapter.WatiNewCallRequest
	if err := json.Unmarshal(body, &request); err != nil {
		logger.Base().Error("Failed to parse new call request payload: %v", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	logger.Base().Info("New call request", zap.String("call_id", request.CallID), zap.String("tenant_id", request.TenantID), zap.Int("sdp_length", len(request.SDP)))

	// Validate required fields
	if request.TenantID == "" {
		logger.Base().Error("Missing tenantId in new call request")
		response := httpadapter.WatiAPIResponse{Code: 400, Message: "tenantId is required"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if request.CallID == "" {
		logger.Base().Error("Missing callId in new call request")
		response := httpadapter.WatiAPIResponse{Code: 400, Message: "callId is required"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if request.SDP == "" {
		logger.Base().Error("Missing sdp in new call request")
		response := httpadapter.WatiAPIResponse{Code: 400, Message: "sdp is required"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Usage gating: check tenant allowance before proceeding
	if h.agentService != nil {
		if allowed, msg := h.agentService.CheckTenantUsageAllowed(context.Background(), request.TenantID); !allowed {
			logger.Base().Error("Usage not allowed for tenant", zap.String("tenant_id", request.TenantID), zap.String("message", msg))
			response := httpadapter.WatiAPIResponse{Code: 403, Message: fmt.Sprintf("Usage not allowed: %s", msg)}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(response)
			return
		}
	}

	// Process the new call
	// Create new connection for this call
	connectionID := fmt.Sprintf("wati_%s_%d", request.CallID, time.Now().UnixNano())
	connection := &call.WhatsAppCallConnection{
		ID:            connectionID,
		CallID:        request.CallID,
		From:          "", // Not provided in this API
		To:            "", // Not provided in this API
		CreatedAt:     time.Now(),
		LastActivity:  time.Now(),
		IsActive:      true,
		StopKeepalive: make(chan struct{}),
		RemoteSDP:     request.SDP,
	}

	// Store connection
	h.service.AddConnection(connection)

	// Initialize voice conversation for this connection
	if err := connection.InitializeVoiceConversation(); err != nil {
		logger.Base().Warn("Failed to initialize voice conversation: %v", zap.Error(err))
		// Continue anyway, AddMessage will create it as fallback
	}

	// Handle session initialization via Task Bus
	if h.taskBus != nil {
		logger.Base().Info("Enqueuing new-call task", zap.String("conn_id", connectionID))
		h.taskBus.Publish(r.Context(), task.SessionTask{
			Type:         task.TaskTypeInboundCall,
			ConnectionID: connectionID,
			Payload:      body,
		})
	} else {
		// Fallback
		go h.processCallOffer(request.TenantID, request.CallID, request.SDP, connection)
		go h.service.InitializeAIConnection(connection)
	}

	logger.Base().Info("✅ New call connection created: %s", zap.String("value", connectionID))

	// Return success response
	response := httpadapter.WatiAPIResponse{Code: 200, Message: "Call accepted successfully"}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleTerminateCall godoc
// @Summary Terminate call
// @Description Terminate an active call
// @Tags wati
// @Accept json
// @Produce json
// @Param call body object true "Terminate call request with tenantId and callId"
// @Success 200 {object} object "Call terminated successfully"
// @Failure 400 {object} object "Bad request - missing required fields"
// @Failure 500 {object} object "Internal server error"
// @Router /wati/terminate-call [post]
func (h *WatiWebhookHandler) handleTerminateCall(w http.ResponseWriter, r *http.Request) {
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Base().Error("Failed to read terminate call request body: %v", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	logger.Base().Info("Terminate call request received", zap.String("body", string(body)))

	// Parse request payload
	var request httpadapter.WatiTerminateCallRequest
	if err := json.Unmarshal(body, &request); err != nil {
		logger.Base().Error("Failed to parse terminate call request payload: %v", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	logger.Base().Info("Terminate call request", zap.String("call_id", request.CallID), zap.String("tenant_id", request.TenantID))

	// Validate required fields
	if request.CallID == "" {
		logger.Base().Error("Missing callId in terminate call request")
		response := httpadapter.WatiAPIResponse{Code: 400, Message: "callId is required"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Find and cleanup connection (via broadcast)
	if err := h.service.NotifyCleanupByCallID(r.Context(), request.CallID); err != nil {
		logger.Base().Error("Failed to broadcast terminate-call task", zap.Error(err))
	} else {
		logger.Base().Info("Broadcast cleanup for terminated call", zap.String("call_id", request.CallID))
	}

	// Return success response immediately
	response := httpadapter.WatiAPIResponse{Code: 200, Message: "Call termination initiated"}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// processCallOffer processes the SDP offer and accepts the call
func (h *WatiWebhookHandler) processCallOffer(tenantID, callID, offerSDP string, connection *call.WhatsAppCallConnection) {
	logger.Base().Info("Processing call offer for: %s (Tenant: %s)", zap.String("call_id", callID), zap.String("tenant_id", tenantID))

	var sdpAnswer string
	var err error

	// Check if WebRTC processor is available
	webrtcProcessor := h.service.GetWebRTCProcessor()
	if webrtcProcessor == nil {
		logger.Base().Error("WebRTC processor is nil, falling back to old SDP generation")
		// Fall back to old method
		sdpAnswer, err = h.watiClient.GenerateSDPAnswer(offerSDP)
		if err != nil {
			logger.Base().Error("Failed to generate SDP answer for call %s: %v", zap.String("call_id", callID), zap.Error(err))
			return
		}
	} else {
		// Process SDP offer with WebRTC
		logger.Base().Info("Using WebRTC processor for SDP offer")
		sdpAnswer, err = webrtcProcessor.ProcessSDPOffer(connection.ID, offerSDP)
		if err != nil {
			logger.Base().Error("Failed to process SDP offer for call %s: %v", zap.String("call_id", callID), zap.Error(err))
			return
		}
	}

	// Store the SDP answer
	connection.SDPAnswer = sdpAnswer
	connection.LocalSDP = sdpAnswer

	// CRITICAL: Wait for Wati API accept to return 200 OK before starting audio
	logger.Base().Info("Calling Wati API accept and waiting for 200 OK...")
	if err := h.watiClient.AcceptCallWithTenant(tenantID, callID, sdpAnswer); err != nil {
		logger.Base().Error("Wati API failed: %v", zap.Error(err))
		logger.Base().Info("NOT starting audio - no 200 OK from accept")
		return
	}

	logger.Base().Info("Wati API accept returned 200 OK")
	logger.Base().Info("NOW starting audio processing (after 200 OK)")

	// Start audio processing ONLY after 200 OK
	go h.startAudioProcessing(connection)
}

// startAudioProcessing starts processing audio for the call using Plan B approach
func (h *WatiWebhookHandler) startAudioProcessing(connection *call.WhatsAppCallConnection) {
	logger.Base().Info("Starting audio processing for Wati call: %s", zap.String("connection_id", connection.ID))

	// Mark as audio active
	connection.HasInboundAudio = true

	// Start AI audio processing
	go h.startAIAudioProcessing(connection)
}

// startAIAudioProcessing replaces test audio with real AI processing
func (h *WatiWebhookHandler) startAIAudioProcessing(connection *call.WhatsAppCallConnection) {
	logger.Base().Info("AI AUDIO: Starting real-time processing for %s", zap.String("connection_id", connection.ID))

	// Check if everything is already ready (no sleep needed)
	if connection.WAOutputTrack != nil && connection.IsAIReady {
		logger.Base().Info("AI AUDIO: Ready for bidirectional audio processing")
		logger.Base().Info("AI AUDIO: Listening for incoming audio from WhatsApp")
		logger.Base().Info("AI AUDIO: Will send AI responses via Pion WriteSample")
		return
	}

	logger.Base().Info("Components not ready yet, will be handled by event system: %s", zap.String("connection_id", connection.ID))
	// Note: Audio processing will start automatically when both AI and WhatsApp audio are ready via events
}

// handleManualAccept godoc
// @Summary Manually accept call (testing)
// @Description Manually accept a call with SDP answer (for testing purposes)
// @Tags wati
// @Accept json
// @Produce json
// @Param callId path string true "Call ID"
// @Param request body object true "Manual accept request with tenantId and SDP"
// @Success 200 {object} object "Call accepted"
// @Failure 400 {string} string "Bad request"
// @Failure 500 {string} string "Internal server error"
// @Router /wati/calls/{callId}/accept [post]
func (h *WatiWebhookHandler) handleManualAccept(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	callID := vars["callId"]

	if callID == "" {
		http.Error(w, "Call ID is required", http.StatusBadRequest)
		return
	}

	// Read SDP from request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var request httpadapter.WatiManualCallRequest
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if request.TenantID == "" {
		http.Error(w, "Tenant ID is required", http.StatusBadRequest)
		return
	}

	// Accept call via Wati using the provided tenant ID
	if err := h.watiClient.AcceptCallWithTenant(request.TenantID, callID, request.SDP); err != nil {
		logger.Base().Error("Failed to accept call manually: %v", zap.Error(err))
		http.Error(w, "Failed to accept call", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "accepted"}`))
}

// handleManualTerminate godoc
// @Summary Manually terminate call (testing)
// @Description Manually terminate a call (for testing purposes)
// @Tags wati
// @Accept json
// @Produce json
// @Param callId path string true "Call ID"
// @Param request body object true "Manual terminate request with tenantId"
// @Success 200 {object} object "Call terminated"
// @Failure 400 {string} string "Bad request"
// @Failure 500 {string} string "Internal server error"
// @Router /wati/calls/{callId}/terminate [post]
func (h *WatiWebhookHandler) handleManualTerminate(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	callID := vars["callId"]

	if callID == "" {
		http.Error(w, "Call ID is required", http.StatusBadRequest)
		return
	}

	// Read request body to get tenant ID
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var request httpadapter.WatiManualCallRequest
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if request.TenantID == "" {
		http.Error(w, "Tenant ID is required", http.StatusBadRequest)
		return
	}

	// Terminate call via Wati using the provided tenant ID
	if err := h.watiClient.TerminateCallWithTenant(request.TenantID, callID); err != nil {
		logger.Base().Error("Failed to terminate call manually: %v", zap.Error(err))
		http.Error(w, "Failed to terminate call", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "terminated"}`))
}

// handleStatus godoc
// @Summary Get service status
// @Description Get the current status of the Wati webhook service including active connections
// @Tags wati
// @Accept json
// @Produce json
// @Success 200 {object} object "Service status information"
// @Router /wati/status [get]
func (h *WatiWebhookHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	connectionCount := h.service.GetConnectionCount()

	status := map[string]interface{}{
		"status":      "running",
		"service":     "wati-whatsapp-call-gateway",
		"timestamp":   time.Now().Format(time.RFC3339),
		"connections": connectionCount,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// handleHealth godoc
// @Summary Health check
// @Description Check if the Wati webhook service is healthy and running
// @Tags wati
// @Accept json
// @Produce json
// @Success 200 {object} object "Service is healthy"
// @Router /wati/health [get]
func (h *WatiWebhookHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "healthy"}`))
}

// Helper methods for webhook processing

// logWebhookRequest logs detailed webhook request information
func (h *WatiWebhookHandler) logWebhookRequest(r *http.Request) {
	logger.Base().Info("WATI WEBHOOK RECEIVED")
	logger.Base().Info("Method", zap.String("method", r.Method))
	logger.Base().Info("URL", zap.String("url", r.URL.String()))
	logger.Base().Info("Remote Address", zap.String("remote_addr", r.RemoteAddr))
	logger.Base().Info("User-Agent", zap.String("user_agent", r.Header.Get("User-Agent")))

	// Print all headers
	logger.Base().Info("Headers")
	for name, values := range r.Header {
		for _, value := range values {
			logger.Base().Debug("Header", zap.String("name", name), zap.String("value", value))
		}
	}
}

// logWebhookBody logs the webhook request body
func (h *WatiWebhookHandler) logWebhookBody(body []byte) {
	logger.Base().Info("Raw Body", zap.Int("bytes", len(body)), zap.String("body", string(body)))

	// Try to format JSON for better readability
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, body, "", "  "); err == nil {
		logger.Base().Info("Formatted JSON", zap.String("json", prettyJSON.String()))
	} else {
		logger.Base().Warn("Could not format as JSON", zap.Error(err))
	}
}

// validateWebhookSignature validates the webhook signature
func (h *WatiWebhookHandler) validateWebhookSignature(r *http.Request, body []byte) bool {
	signature := r.Header.Get("X-Hub-Signature-256")
	if signature != "" {
		if !h.verifyWebhookSignature(body, signature) {
			logger.Base().Error("Invalid webhook signature")
			return false
		}
		logger.Base().Info("Webhook signature verified")
	} else {
		logger.Base().Warn("No webhook signature provided")
	}
	return true
}

// parseWebhookEvent parses the webhook event from JSON body and extracts API Key from Authorization header
func (h *WatiWebhookHandler) parseWebhookEvent(body []byte, r *http.Request) (httpadapter.WatiWebhookEvent, error) {
	var event httpadapter.WatiWebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		logger.Base().Error("Failed to parse Wati webhook payload", zap.Error(err))
		return event, err
	}

	// Extract API Key from Authorization header (C# format: "Bearer <apiKey>")
	// Priority: Authorization header > event.APIKey from body
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		// Check if it starts with "Bearer "
		if strings.HasPrefix(authHeader, "Bearer ") {
			apiKey := strings.TrimPrefix(authHeader, "Bearer ")
			event.APIKey = apiKey
			logger.Base().Info("🔑 API Key extracted from Authorization header: %s", zap.String("value", apiKey))
		} else {
			logger.Base().Info("⚠️  Authorization header format incorrect: %s", zap.String("value", authHeader))
		}
	} else if event.APIKey != "" {
		logger.Base().Info("API Key found in request body", zap.String("api_key", event.APIKey))
	} else {
		logger.Base().Warn("No API Key provided in header or body")
	}

	// If APIKey is present and AgentMapping is nil, decode JWT to populate AgentMapping
	if event.APIKey != "" && event.AgentMapping == nil {
		logger.Base().Info("Decoding JWT from APIKey for call", zap.String("call_id", event.CallID))
		if agentMapping, err := httpadapter.DecodeJWTFromAPIKey(event.APIKey); err == nil {
			event.AgentMapping = agentMapping
			logger.Base().Info("Successfully decoded AgentMapping from JWT", zap.String("tenant_id", agentMapping.TenantID), zap.String("agent_id", agentMapping.AgentID))

			// If TenantID was not provided in the webhook body, use the one from JWT
			if event.TenantID == "" {
				event.TenantID = agentMapping.TenantID
				logger.Base().Info("Set TenantID from JWT", zap.String("tenant_id", event.TenantID))
			}
		} else {
			logger.Base().Warn("Failed to decode JWT from APIKey: %v", zap.Error(err))
		}
	}

	logger.Base().Info("Wati webhook event for call", zap.String("call_id", event.CallID), zap.String("tenant_id", event.TenantID), zap.String("business_number", event.BusinessNumber))
	return event, nil
}

// isDuplicateWebhook checks if this webhook has already been processed recently
func (h *WatiWebhookHandler) isDuplicateWebhook(event httpadapter.WatiWebhookEvent) bool {
	webhookKey := fmt.Sprintf("call_%s_%s", event.CallID, event.TenantID)
	if processedTime, exists := h.processedWebhooks[webhookKey]; exists {
		// If processed within last 30 seconds, consider it a duplicate
		if time.Since(processedTime) < 30*time.Second {
			logger.Base().Warn("Duplicate webhook detected, ignoring", zap.String("webhook_key", webhookKey), zap.Duration("processed_ago", time.Since(processedTime)))
			return true
		}
	}

	// Mark webhook as processed
	h.processedWebhooks[webhookKey] = time.Now()

	// Clean up old processed webhooks (older than 5 minutes)
	h.cleanupOldWebhooks()
	return false
}

// cleanupOldWebhooks removes old webhook processing records
func (h *WatiWebhookHandler) cleanupOldWebhooks() {
	for key, processedTime := range h.processedWebhooks {
		if time.Since(processedTime) > 5*time.Minute {
			delete(h.processedWebhooks, key)
		}
	}
}

// processWebhookEvent processes the webhook event based on its content
func (h *WatiWebhookHandler) processWebhookEvent(event httpadapter.WatiWebhookEvent, body []byte) {
	// If we have SDP data, treat it as a call start (incoming call)
	if len(event.SDP) > 0 {
		logger.Base().Info("Incoming call detected with SDP data")
		h.handleCallStart(event, body)
	} else {
		logger.Base().Warn("No SDP data found in webhook")
	}
}

// handleWebCallEndpoint handles the /web-new-call route
func (h *WatiWebhookHandler) handleWebCallEndpoint(w http.ResponseWriter, r *http.Request) {
	h.handleWebNewCall(w, r, domain.ChannelTypeWeb)
}

// handleWebNewCall processes new call requests for Web and Test channels
func (h *WatiWebhookHandler) handleWebNewCall(w http.ResponseWriter, r *http.Request, channelType domain.ChannelType) {
	logger.Base().Info("New call request received", zap.String("channel_type", string(channelType)))

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Base().Error("Failed to read request body: %v", zap.String("channel_type", string(channelType)), zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	logger.Base().Info("Request body", zap.String("channel_type", string(channelType)), zap.String("body", string(body)))

	// Parse request payload
	var request struct {
		CallID         string `json:"callId"`
		TenantID       string `json:"tenantId"` // Optional in test mode
		SDP            string `json:"sdp"`
		AgentId        string `json:"agentId"`
		BusinessNumber string `json:"businessNumber"`
		ContactName    string `json:"contactName"`
		From           string `json:"from"`
		Language       string `json:"language"`
		Accent         string `json:"accent"`
		ModelProvider  string `json:"modelProvider"`
	}

	if err := json.Unmarshal(body, &request); err != nil {
		logger.Base().Error("Failed to parse request: %v", zap.String("channel_type", string(channelType)), zap.Error(err))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if request.AgentId == "" {
		logger.Base().Info("[%s MODE] Missing agentId", zap.String("channel_type", string(channelType)))
		http.Error(w, "AgentID is required", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if request.CallID == "" {
		logger.Base().Info("❌ [%s MODE] Missing callId", zap.String("channel_type", string(channelType)))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    400,
			"message": "callId is required",
		})
		return
	}

	if request.SDP == "" {
		logger.Base().Info("❌ [%s MODE] Missing sdp", zap.String("channel_type", string(channelType)))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    400,
			"message": "sdp is required",
		})
		return
	}

	// Use default tenant ID if not provided
	tenantID := request.TenantID
	if tenantID == "" {
		tenantID = config.DefaultTenantID
		logger.Base().Info("No tenantId provided, using default: %s", zap.String("channel_type", string(channelType)), zap.String("tenant_id", tenantID))
	}

	// Usage gating: check tenant allowance before proceeding
	if tenantID != "" && tenantID != config.DefaultTenantID && tenantID != config.DefaultWatiTenantID && h.agentService != nil {
		if allowed, msg := h.agentService.CheckTenantUsageAllowed(context.Background(), tenantID); !allowed {
			logger.Base().Error("Usage not allowed for tenant", zap.String("channel_type", string(channelType)), zap.String("tenant_id", tenantID), zap.String("message", msg))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code":    403,
				"message": fmt.Sprintf("Usage not allowed: %s", msg),
			})
			return
		}
	}
	agentID := request.AgentId
	voiceLanguage := "en"
	var textAgentID string

	if agentID != "" {
		agent, _ := h.agentService.GetAgentConfigWithChannelType(context.Background(), agentID, channelType)
		if agent != nil {
			voiceLanguage = agent.Language
			textAgentID = agent.TextAgentID
		} else {
			http.Error(w, "Agent not found", http.StatusBadRequest)
			return
		}
	}

	logger.Base().Info("Creating call", zap.String("channel_type", string(channelType)), zap.String("call_id", request.CallID), zap.String("tenant_id", tenantID), zap.Int("sdp_length", len(request.SDP)), zap.String("agent_id", agentID))

	if request.Language != "" {
		voiceLanguage = request.Language
	}
	// Determine model provider (default to OpenAI)
	modelProvider := provider.ProviderTypeOpenAI
	if request.ModelProvider == string(provider.ProviderTypeGemini) {
		modelProvider = provider.ProviderTypeGemini
	}

	// Create new connection for this call
	connectionID := fmt.Sprintf("%s_%s_%d", channelType, request.CallID, time.Now().UnixNano())
	connection := &call.WhatsAppCallConnection{
		ID:            connectionID,
		CallID:        request.CallID,
		From:          request.From, // Phone number
		To:            "",           // Not provided
		CreatedAt:     time.Now(),
		LastActivity:  time.Now(),
		IsActive:      true,
		ChannelType:   channelType, // Test or Web channel needs audio caching
		StopKeepalive: make(chan struct{}),
		RemoteSDP:     request.SDP,
		TenantID:      tenantID,
		AgentID:       agentID,
		TextAgentID:   textAgentID,
		VoiceLanguage: voiceLanguage,
		Accent:        request.Accent,
		CountryCode:   "US",
		ContactName:   request.ContactName,
		RepoManager:   h.repoManager, // Add repository manager for database operations
		ModelProvider: modelProvider,
	}

	// Store connection
	h.service.AddConnection(connection)

	// Initialize voice conversation for this connection
	if err := connection.InitializeVoiceConversation(); err != nil {
		logger.Base().Warn("Failed to initialize voice conversation: %v", zap.Error(err))
		// Continue anyway, AddMessage will create it as fallback
	}

	logger.Base().Info("Connection created: %s", zap.String("channel_type", string(channelType)), zap.String("connection_id", connectionID))

	// Process SDP offer SYNCHRONOUSLY to get the answer before responding
	var sdpAnswer string

	// Check if WebRTC processor is available
	webrtcProcessor := h.service.GetWebRTCProcessor()
	if webrtcProcessor == nil {
		logger.Base().Info("❌ [%s MODE] WebRTC processor is nil", zap.String("channel_type", string(channelType)))
		http.Error(w, "WebRTC processor not available", http.StatusInternalServerError)
		return
	}

	// Process SDP offer with WebRTC
	logger.Base().Info("🔄 [%s MODE] Using WebRTC processor for SDP offer", zap.String("channel_type", string(channelType)))
	var procErr error
	sdpAnswer, procErr = webrtcProcessor.ProcessSDPOffer(connection.ID, request.SDP)
	if procErr != nil {
		logger.Base().Error("Failed to process SDP offer: %v", zap.String("channel_type", string(channelType)), zap.Any("procerr", procErr))
		http.Error(w, fmt.Sprintf("Failed to process SDP: %v", procErr), http.StatusInternalServerError)
		return
	}

	// Store the SDP answer
	connection.SDPAnswer = sdpAnswer
	connection.LocalSDP = sdpAnswer

	logger.Base().Info("SDP Answer generated", zap.String("channel_type", string(channelType)), zap.Int("length", len(sdpAnswer)))

	// Handle session initialization via Task Bus
	if h.taskBus != nil {
		logger.Base().Info("Enqueuing web-call task", zap.String("conn_id", connectionID))
		h.taskBus.Publish(r.Context(), task.SessionTask{
			Type:         task.TaskTypeWebCall,
			ConnectionID: connectionID,
			Payload:      body,
		})
	} else {
		// Fallback
		go h.startAudioProcessing(connection)
		go h.service.InitializeAIConnection(connection)
	}

	logger.Base().Info("Call connection created: %s", zap.String("channel_type", string(channelType)), zap.String("connection_id", connectionID))

	// Return success response with original SDP Answer (TURN relay handles connectivity)
	response := map[string]interface{}{
		"code":         200,
		"message":      "Call accepted successfully (TURN relay mode)",
		"connectionId": connectionID,
		"callId":       request.CallID,
		"sdpAnswer":    sdpAnswer, // Original SDP - TURN relay handles all connectivity
		"testMode":     channelType == domain.ChannelTypeTest,
		"relayMode":    true, // Indicate using TURN relay
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ========================================
// TEST MODE ENDPOINTS
// ========================================

// handleTestNewCall godoc
// @Summary Create new call (TEST MODE)
// @Description Test mode endpoint - bypasses Wati API and tenant validation for WebRTC testing
// @Tags wati-test
// @Accept json
// @Produce json
// @Param call body object true "Test call request with callId, SDP offer and modelProvider (tenantId optional)"
// @Success 200 {object} object "Call accepted with SDP answer"
// @Failure 400 {object} object "Bad request"
// @Failure 500 {object} object "Internal server error"
// @Router /wati/test/new-call [post]
func (h *WatiWebhookHandler) handleTestNewCall(w http.ResponseWriter, r *http.Request) {
	logger.Base().Info("New call request received")
	h.handleWebNewCall(w, r, domain.ChannelTypeTest)
}

// handleTestTerminateCall godoc
// @Summary Terminate call (TEST MODE)
// @Description Test mode endpoint - terminates call without calling Wati API
// @Tags wati-test
// @Accept json
// @Produce json
// @Param call body object true "Test terminate request with callId"
// @Success 200 {object} object "Call terminated"
// @Failure 400 {object} object "Bad request"
// @Router /wati/test/terminate-call [post]
func (h *WatiWebhookHandler) handleTestTerminateCall(w http.ResponseWriter, r *http.Request) {
	logger.Base().Info("Terminate call request received")

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Base().Error("[TEST MODE] Failed to read request body: %v", zap.Error(err))
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Parse request payload
	var request struct {
		CallID string `json:"callId"`
	}

	if err := json.Unmarshal(body, &request); err != nil {
		logger.Base().Error("[TEST MODE] Failed to parse request: %v", zap.Error(err))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if request.CallID == "" {
		logger.Base().Error("Missing callId")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    400,
			"message": "callId is required",
		})
		return
	}

	logger.Base().Info("Terminating call", zap.String("call_id", request.CallID))

	// Find and cleanup connection (via broadcast)
	if err := h.service.NotifyCleanupByCallID(r.Context(), request.CallID); err != nil {
		logger.Base().Error("Failed to broadcast terminate-call task", zap.Error(err))
	} else {
		logger.Base().Info("Broadcast cleanup for test call", zap.String("call_id", request.CallID))
	}

	// Return success response immediately
	response := map[string]interface{}{
		"code":     200,
		"message":  "Test call termination initiated",
		"callId":   request.CallID,
		"testMode": true,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// processTestCallOffer processes the SDP offer for test calls (without Wati API)
func (h *WatiWebhookHandler) processTestCallOffer(callID, offerSDP string, connection *call.WhatsAppCallConnection) {
	logger.Base().Info("🧪 [TEST MODE] Processing call offer for: %s", zap.String("value", callID))

	var sdpAnswer string
	var err error

	// Check if WebRTC processor is available
	webrtcProcessor := h.service.GetWebRTCProcessor()
	if webrtcProcessor == nil {
		logger.Base().Error("WebRTC processor is nil")
		return
	}

	// Process SDP offer with WebRTC
	logger.Base().Info("Using WebRTC processor for SDP offer")
	sdpAnswer, err = webrtcProcessor.ProcessSDPOffer(connection.ID, offerSDP)
	if err != nil {
		logger.Base().Error("[TEST MODE] Failed to process SDP offer: %v", zap.Error(err))
		return
	}

	// Store the SDP answer
	connection.SDPAnswer = sdpAnswer
	connection.LocalSDP = sdpAnswer

	logger.Base().Info("SDP Answer generated", zap.Int("length", len(sdpAnswer)))
	logger.Base().Info("Starting audio processing (no Wati API call)")

	// Start audio processing directly (without waiting for Wati API)
	go h.startAudioProcessing(connection)
}
