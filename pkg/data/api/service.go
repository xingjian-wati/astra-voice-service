package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
	"io"
	"net/http"
	"time"
)

// CreateContactRequest represents the payload for creating a contact
type CreateContactRequest struct {
	AgentID         string `json:"agent_id"`
	ConversationID  string `json:"conversation_id,omitempty"`
	Mode            string `json:"mode"`
	InteractionType string `json:"interaction_type"`
	TenantID        string `json:"tenant_id"`
	PhoneNumber     string `json:"phone_number"`
}

// CreateContactResponse represents the success response from the contact creation API
type CreateContactResponse struct {
	ContactID string `json:"contact_id"`
}

// ErrorResponse represents the error response from the API
type ErrorResponse struct {
	Error string `json:"error"`
}

// SyncVoiceChatRequest represents the payload for syncing a voice chat
type SyncVoiceChatRequest struct {
	AgentID        string `json:"agent_id" binding:"required"`        // Platform agent ID
	ConversationID string `json:"conversation_id" binding:"required"` // Voice conversation ID (internal ID from voice_conversations table)
	ContactID      string `json:"contact_id,omitempty"`               // Contact ID (created externally)
}

// SyncVoiceChatResponse represents the response from the sync voice chat API
type SyncVoiceChatResponse struct {
	Success        bool   `json:"success"`
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
	MessagesCount  int    `json:"messages_count"`
}

// LeadsService handles interactions with the leads/contacts API
type LeadsService struct {
	baseURL string
	client  *http.Client
}

// NewLeadsService creates a new LeadsService
func NewLeadsService(baseURL string) *LeadsService {
	return &LeadsService{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CreateContact pushes conversation configuration to create a contact
// It calls POST /api/v2/contacts
func (s *LeadsService) CreateContact(req CreateContactRequest) (*CreateContactResponse, error) {
	url := fmt.Sprintf("%s/api/v2/contacts", s.baseURL)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := s.client.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode > http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Base().Error("Leads API error", zap.Int("status_code", resp.StatusCode), zap.String("body", string(bodyBytes)))

		var errorResp ErrorResponse
		if json.Unmarshal(bodyBytes, &errorResp) == nil && errorResp.Error != "" {
			return nil, fmt.Errorf("api error: %s", errorResp.Error)
		}
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var response CreateContactResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// SyncVoiceChat syncs voice chat data
// It calls POST /api/v2/voice-agent/sync
func (s *LeadsService) SyncVoiceChat(req SyncVoiceChatRequest) (*SyncVoiceChatResponse, error) {
	url := fmt.Sprintf("%s/api/v2/voice-agent/sync", s.baseURL)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := s.client.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Base().Error("API error", zap.Int("status_code", resp.StatusCode), zap.String("body", string(bodyBytes)))

		var errorResp ErrorResponse
		if json.Unmarshal(bodyBytes, &errorResp) == nil && errorResp.Error != "" {
			return nil, fmt.Errorf("api error: %s", errorResp.Error)
		}
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var response SyncVoiceChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}
