package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
	"io"
	"net/http"
	"strings"
	"time"
)

// RAGRequest represents the request structure for RAG API (old format with app- token)
type RAGRequest struct {
	Inputs         map[string]interface{} `json:"inputs"`
	Query          string                 `json:"query"`
	ResponseMode   string                 `json:"response_mode"`
	ConversationID string                 `json:"conversation_id"`
	User           string                 `json:"user"`
}

// RAGRequestV2 represents the request structure for new RAG API (agent_id format)
type RAGRequestV2 struct {
	AgentID string `json:"agent_id"`
	Query   string `json:"query"`
}

// RAGResponse represents the response structure from RAG API (old format)
type RAGResponse struct {
	Event          string                 `json:"event"`
	TaskID         string                 `json:"task_id"`
	ID             string                 `json:"id"`
	MessageID      string                 `json:"message_id"`
	ConversationID string                 `json:"conversation_id"`
	Mode           string                 `json:"mode"`
	Answer         string                 `json:"answer"` // JSON-encoded array of documents
	Metadata       map[string]interface{} `json:"metadata"`
	CreatedAt      int64                  `json:"created_at"`
	Data           map[string]interface{} `json:"data"` // Keep for backward compatibility
}

// RAGResponseV2 represents the response structure from new RAG API
type RAGResponseV2 struct {
	Query   string `json:"query"`
	Results string `json:"results"`
}

// RAGDocument represents a document in the RAG response
type RAGDocument struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	URL     string `json:"url"`
}

// RAGClient handles RAG requests to knowledge base APIs
type RAGClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewRAGClient creates a new RAG client with custom configuration
func NewRAGClient(baseURL, token string) *RAGClient {
	return &RAGClient{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// QueryRAG queries the RAG API with the given question and returns the answer
func (r *RAGClient) QueryRAG(ctx context.Context, query string, userID string, conversationID string) (string, error) {
	if r.baseURL == "" || r.token == "" {
		return "", fmt.Errorf("RAG client not properly configured")
	}

	logger.Base().Debug("RAG Query to : (user: , conversation: )", zap.String("baseurl", r.baseURL))

	// Check if URL contains "dify" to determine which API format to use
	if strings.Contains(strings.ToLower(r.baseURL), "dify") {
		logger.Base().Info("🔹 Using old Dify API format")
		return r.queryRAGOldFormat(ctx, query, userID, conversationID)
	}

	logger.Base().Info("🔹 Using new API format (agent_id)")
	return r.queryRAGNewFormat(ctx, query)
}

// queryRAGOldFormat handles the old RAG API format with app- token
func (r *RAGClient) queryRAGOldFormat(ctx context.Context, query string, userID string, conversationID string) (string, error) {
	// Prepare request payload
	payload := RAGRequest{
		Inputs:         make(map[string]interface{}),
		Query:          query,
		ResponseMode:   "blocking",
		ConversationID: conversationID,
		User:           userID,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", r.baseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.token)

	// Execute request
	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var response RAGResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Extract answer from response
	return r.extractAnswerFromResponse(response)
}

// queryRAGNewFormat handles the new RAG API format with agent_id
func (r *RAGClient) queryRAGNewFormat(ctx context.Context, query string) (string, error) {
	// Prepare request payload with agent_id (token is used as agent_id)
	payload := RAGRequestV2{
		AgentID: r.token,
		Query:   query,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	logger.Base().Info("New API Request", zap.ByteString("json_data", jsonData))

	// Build the full API URL with /api/v2/retrieve endpoint
	apiURL := strings.TrimSuffix(r.baseURL, "/") + "/api/v2/retrieve"
	logger.Base().Info("New API URL", zap.String("apiurl", apiURL))

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers (no Authorization header for new API)
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	logger.Base().Info("New API Response", zap.Int("status_code", resp.StatusCode), zap.String("body", string(body)))

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var response RAGResponseV2
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Return the results field directly
	if response.Results == "" {
		return "", fmt.Errorf("empty results in response")
	}

	return response.Results, nil
}

// extractAnswerFromResponse extracts the answer from different RAG API response formats
func (r *RAGClient) extractAnswerFromResponse(response RAGResponse) (string, error) {
	// Try to extract from new format with answer field (JSON-encoded array)
	if response.Answer != "" {
		// Parse the JSON array of documents
		var documents []RAGDocument
		if err := json.Unmarshal([]byte(response.Answer), &documents); err != nil {
			// If it's not a JSON array, treat it as plain text answer
			logger.Base().Error("Failed to parse answer as JSON array, treating as plain text")
			return response.Answer, nil
		}

		// Extract content from all documents
		if len(documents) > 0 {
			var contents []string
			for _, doc := range documents {
				if doc.Content != "" {
					contents = append(contents, doc.Content)
				}
			}
			if len(contents) > 0 {
				return strings.Join(contents, "\n\n"), nil
			}
		}

		// If documents array is empty, return the raw answer
		return response.Answer, nil
	}

	// Backward compatibility: Try to extract from Dify-style response (WATI format)
	if response.Data != nil {
		if outputs, ok := response.Data["outputs"].(map[string]interface{}); ok {
			if documents, ok := outputs["documents"].([]interface{}); ok && len(documents) > 0 {
				var contents []string
				for _, doc := range documents {
					if docMap, ok := doc.(map[string]interface{}); ok {
						if content, ok := docMap["page_content"].(string); ok {
							contents = append(contents, content)
						}
					}
				}
				if len(contents) > 0 {
					return strings.Join(contents, "\n\n"), nil
				}
			}

			// Try to get direct answer field
			if answer, ok := outputs["answer"].(string); ok {
				return answer, nil
			}

			// Try to get text field
			if text, ok := outputs["text"].(string); ok {
				return text, nil
			}
		}

		// Try to extract from simple response format
		if answer, ok := response.Data["answer"].(string); ok {
			return answer, nil
		}

		if text, ok := response.Data["text"].(string); ok {
			return text, nil
		}

		// If no standard format found, return the raw data as JSON string
		jsonBytes, err := json.Marshal(response.Data)
		if err != nil {
			return "", fmt.Errorf("failed to extract answer from response and failed to marshal data: %w", err)
		}

		return string(jsonBytes), nil
	}

	return "", fmt.Errorf("no answer found in response")
}

// IsConfigured returns true if the RAG client is properly configured
func (r *RAGClient) IsConfigured() bool {
	return r.baseURL != "" && r.token != ""
}
