package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/internal/repository"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// VoiceConversationHandler handles HTTP requests for voice conversations
type VoiceConversationHandler struct {
	conversationRepo *repository.VoiceConversationRepository
	messageRepo      *repository.VoiceMessageRepository
}

// NewVoiceConversationHandler creates a new voice conversation handler
func NewVoiceConversationHandler(conversationRepo *repository.VoiceConversationRepository, messageRepo *repository.VoiceMessageRepository) *VoiceConversationHandler {
	return &VoiceConversationHandler{
		conversationRepo: conversationRepo,
		messageRepo:      messageRepo,
	}
}

// CreateVoiceConversationRequest represents the request to create a voice conversation
type CreateVoiceConversationRequest struct {
	ExternalConversationID string    `json:"external_conversation_id" validate:"required"`
	VoiceAgentID           string    `json:"voice_agent_id" validate:"required"`
	StartedAt              time.Time `json:"started_at"`
	EndedAt                time.Time `json:"ended_at"`
}

// UpdateVoiceConversationRequest represents the request to update a voice conversation
type UpdateVoiceConversationRequest struct {
	VoiceAgentID string    `json:"voice_agent_id,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	EndedAt      time.Time `json:"ended_at,omitempty"`
}

// VoiceConversationResponse represents the response for voice conversation operations
type VoiceConversationResponse struct {
	*domain.VoiceConversation
	Messages []*domain.VoiceMessage `json:"messages,omitempty"`
}

// VoiceConversationListResponse represents the response for listing voice conversations
type VoiceConversationListResponse struct {
	Conversations []*domain.VoiceConversation `json:"conversations"`
	Total         int                         `json:"total"`
	Page          int                         `json:"page"`
	PageSize      int                         `json:"page_size"`
}

// CreateVoiceConversation godoc
// @Summary Create a voice conversation
// @Description Create a new voice conversation record
// @Tags conversations
// @Accept json
// @Produce json
// @Param conversation body CreateVoiceConversationRequest true "Conversation creation request"
// @Success 201 {object} domain.VoiceConversation "Conversation created successfully"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/voice-conversations [post]
func (h *VoiceConversationHandler) CreateVoiceConversation(w http.ResponseWriter, r *http.Request) {
	var req CreateVoiceConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ExternalConversationID == "" {
		http.Error(w, "external_conversation_id is required", http.StatusBadRequest)
		return
	}
	if req.VoiceAgentID == "" {
		http.Error(w, "voice_agent_id is required", http.StatusBadRequest)
		return
	}

	// Set default times if not provided
	if req.StartedAt.IsZero() {
		req.StartedAt = time.Now()
	}
	if req.EndedAt.IsZero() {
		req.EndedAt = time.Now()
	}

	conversation, err := h.conversationRepo.CreateByExternalID(
		r.Context(),
		req.ExternalConversationID,
		req.VoiceAgentID,
		req.StartedAt,
		req.EndedAt,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(conversation)
}

// GetVoiceConversation godoc
// @Summary Get conversation by ID
// @Description Retrieve a voice conversation by ID or external conversation ID
// @Tags conversations
// @Accept json
// @Produce json
// @Param id path string true "Conversation ID (UUID) or external conversation ID"
// @Param include_messages query boolean false "Include conversation messages" default(false)
// @Success 200 {object} VoiceConversationResponse "Conversation found"
// @Failure 404 {object} map[string]string "Conversation not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/voice-conversations/{id} [get]
func (h *VoiceConversationHandler) GetVoiceConversation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Check if we should include messages
	includeMessages := r.URL.Query().Get("include_messages") == "true"

	// First try to get by ID, then by external conversation ID
	var conversation *domain.VoiceConversation
	var err error

	// Try to get by internal ID first
	conversation, err = h.conversationRepo.GetByID(r.Context(), id)
	if err != nil || conversation == nil {
		// Try to get by external conversation ID
		conversation, err = h.conversationRepo.GetByExternalConversationID(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if conversation == nil {
			http.Error(w, "Voice conversation not found", http.StatusNotFound)
			return
		}
	}

	response := &VoiceConversationResponse{
		VoiceConversation: conversation,
	}

	// Include messages if requested
	if includeMessages {
		messages, err := h.messageRepo.GetByConversationID(r.Context(), conversation.ID)
		if err != nil {
			logger.Base().Error("Warning: Failed to get messages for conversation", zap.String("id", conversation.ID))
		} else {
			response.Messages = messages
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetVoiceConversations godoc
// @Summary List voice conversations
// @Description Retrieve a paginated list of voice conversations filtered by agent
// @Tags conversations
// @Accept json
// @Produce json
// @Param voice_agent_id query string true "Filter by voice agent ID"
// @Param start_time query string false "Filter start time (RFC3339 format)" format(date-time)
// @Param end_time query string false "Filter end time (RFC3339 format)" format(date-time)
// @Param page query integer false "Page number" default(1) minimum(1)
// @Param page_size query integer false "Items per page" default(20) minimum(1) maximum(100)
// @Success 200 {object} VoiceConversationListResponse "List of conversations"
// @Failure 400 {object} map[string]string "Invalid parameters"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/voice-conversations [get]
func (h *VoiceConversationHandler) GetVoiceConversations(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	voiceAgentID := r.URL.Query().Get("voice_agent_id")
	startTimeStr := r.URL.Query().Get("start_time")
	endTimeStr := r.URL.Query().Get("end_time")
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("page_size")
	source := r.URL.Query().Get("source")

	// Set default pagination
	page := 1
	pageSize := 20

	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}

	// Parse time filters
	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			http.Error(w, "Invalid start_time format, use RFC3339", http.StatusBadRequest)
			return
		}
	} else {
		// Default to last 30 days
		startTime = time.Now().AddDate(0, 0, -30)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			http.Error(w, "Invalid end_time format, use RFC3339", http.StatusBadRequest)
			return
		}
	} else {
		endTime = time.Now()
	}
	var voiceSource domain.ConversationSource
	if source != "" {
		voiceSource = domain.ConversationSource(source)
	} else {
		voiceSource = domain.ConversationSourceTest
	}

	var conversations []*domain.VoiceConversation

	if voiceAgentID != "" {
		conversations, err = h.conversationRepo.FindByVoiceAgentID(r.Context(), voiceAgentID, startTime, endTime, voiceSource)
	} else {
		// If no voice agent ID specified, return error for now
		// We could implement a GetAll method in the repository if needed
		http.Error(w, "voice_agent_id parameter is required", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Simple pagination (in-memory)
	total := len(conversations)
	start := (page - 1) * pageSize
	end := start + pageSize

	if start >= total {
		conversations = []*domain.VoiceConversation{}
	} else {
		if end > total {
			end = total
		}
		conversations = conversations[start:end]
	}

	response := &VoiceConversationListResponse{
		Conversations: conversations,
		Total:         total,
		Page:          page,
		PageSize:      pageSize,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateVoiceConversation godoc
// @Summary Update a conversation
// @Description Update an existing voice conversation
// @Tags conversations
// @Accept json
// @Produce json
// @Param id path string true "Conversation ID (UUID) or external conversation ID"
// @Param conversation body UpdateVoiceConversationRequest true "Conversation update request"
// @Success 200 {object} domain.VoiceConversation "Conversation updated successfully"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 404 {object} map[string]string "Conversation not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/voice-conversations/{id} [put]
func (h *VoiceConversationHandler) UpdateVoiceConversation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req UpdateVoiceConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get existing conversation
	conversation, err := h.conversationRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if conversation == nil {
		// Try by external conversation ID
		conversation, err = h.conversationRepo.GetByExternalConversationID(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if conversation == nil {
			http.Error(w, "Voice conversation not found", http.StatusNotFound)
			return
		}
	}

	// Update fields if provided
	if req.VoiceAgentID != "" {
		conversation.VoiceAgentID = req.VoiceAgentID
	}
	if !req.StartedAt.IsZero() {
		conversation.StartedAt = req.StartedAt
	}
	if !req.EndedAt.IsZero() {
		conversation.EndedAt = req.EndedAt
	}

	err = h.conversationRepo.Update(r.Context(), conversation)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(conversation)
}

// DeleteVoiceConversation godoc
// @Summary Delete a conversation
// @Description Delete a voice conversation and all its associated messages
// @Tags conversations
// @Accept json
// @Produce json
// @Param id path string true "Conversation ID (UUID) or external conversation ID"
// @Success 204 "Conversation deleted successfully"
// @Failure 404 {object} map[string]string "Conversation not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/voice-conversations/{id} [delete]
func (h *VoiceConversationHandler) DeleteVoiceConversation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Get existing conversation to get the internal ID
	conversation, err := h.conversationRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if conversation == nil {
		// Try by external conversation ID
		conversation, err = h.conversationRepo.GetByExternalConversationID(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if conversation == nil {
			http.Error(w, "Voice conversation not found", http.StatusNotFound)
			return
		}
	}

	// Delete associated messages first
	err = h.messageRepo.DeleteByConversationID(r.Context(), conversation.ID)
	if err != nil {
		logger.Base().Error("Warning: Failed to delete messages for conversation", zap.String("id", conversation.ID))
	}

	// Delete conversation (implement this method in repository)
	err = h.conversationRepo.Delete(r.Context(), conversation.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetVoiceConversationMessages godoc
// @Summary Get conversation messages
// @Description Retrieve all messages for a specific voice conversation
// @Tags conversations
// @Accept json
// @Produce json
// @Param id path string true "Conversation ID (UUID) or external conversation ID"
// @Success 200 {object} map[string]interface{} "Conversation messages" example({"conversation_id": "uuid", "messages": [], "total": 0})
// @Failure 404 {object} map[string]string "Conversation not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/voice-conversations/{id}/messages [get]
func (h *VoiceConversationHandler) GetVoiceConversationMessages(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Get conversation to verify it exists and get internal ID
	conversation, err := h.conversationRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if conversation == nil {
		// Try by external conversation ID
		conversation, err = h.conversationRepo.GetByExternalConversationID(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if conversation == nil {
			http.Error(w, "Voice conversation not found", http.StatusNotFound)
			return
		}
	}

	messages, err := h.messageRepo.GetByConversationID(r.Context(), conversation.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"conversation_id": conversation.ID,
		"messages":        messages,
		"total":           len(messages),
	})
}

// CheckVoiceConversationExists godoc
// @Summary Check if conversation exists
// @Description Check whether a voice conversation with the specified ID or external conversation ID exists
// @Tags conversations
// @Accept json
// @Produce json
// @Param id path string true "Conversation ID (UUID) or external conversation ID"
// @Success 200 "Conversation exists"
// @Failure 404 "Conversation not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/voice-conversations/{id} [head]
func (h *VoiceConversationHandler) CheckVoiceConversationExists(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Try both internal ID and external conversation ID
	conversation, err := h.conversationRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if conversation == nil {
		conversation, err = h.conversationRepo.GetByExternalConversationID(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if conversation != nil {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// SetupVoiceConversationRoutes sets up all voice conversation-related routes
func (h *VoiceConversationHandler) SetupVoiceConversationRoutes(router *mux.Router) {
	// Voice conversation CRUD routes
	router.HandleFunc("/voice-conversations", h.CreateVoiceConversation).Methods("POST")
	router.HandleFunc("/voice-conversations", h.GetVoiceConversations).Methods("GET")
	router.HandleFunc("/voice-conversations/{id}", h.GetVoiceConversation).Methods("GET")
	router.HandleFunc("/voice-conversations/{id}", h.UpdateVoiceConversation).Methods("PUT")
	router.HandleFunc("/voice-conversations/{id}", h.DeleteVoiceConversation).Methods("DELETE")
	router.HandleFunc("/voice-conversations/{id}", h.CheckVoiceConversationExists).Methods("HEAD")

	// Voice conversation messages routes
	router.HandleFunc("/voice-conversations/{id}/messages", h.GetVoiceConversationMessages).Methods("GET")

	logger.Base().Info("Voice conversation routes registered")
}
