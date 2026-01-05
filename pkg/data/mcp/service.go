package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// ComposioService handles interactions with Composio MCP service
type ComposioService struct {
	BaseURL    string
	httpClient *http.Client
}
type ActionType string

const (
	ActionTypeInternal ActionType = "internal"
	ActionTypeExternal ActionType = "external"
)

type IntegratedAction struct {
	ActionID     string     `json:"action_id"`     // Configured action ID from database, empty if not configured
	ActionName   string     `json:"action_name"`   // Configured action name from database, empty if not configured
	AtID         string     `json:"at_id"`         // Action template ID from MCP service (always present)
	AtName       string     `json:"at_name"`       // Action template name from MCP service (always present)
	McpTool      string     `json:"mcp_tool"`      // MCP tool identifier for external execution, empty for internal/local actions
	Platform     string     `json:"platform"`      // Platform name (e.g., "hubspot", "slack"), empty for internal actions
	Type         ActionType `json:"type"`          // internal or external
	ConnectionID string     `json:"connection_id"` // Connection ID for external actions, empty if not connected
}

const (
	ModalityVoice         = "voice"
	ModalityText          = "text"
	ModalityVoiceInbound  = "voice_inbound"
	ModalityVoiceOutbound = "voice_outbound"
)

// NewComposioService creates a new ComposioService instance
func NewComposioService(baseURL string) *ComposioService {
	return &ComposioService{
		BaseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// PlatformActionsResponse represents the response structure from /mcp/info endpoint
type PlatformActionsResponse struct {
	PlatformTools map[string][]string `json:"platform_tools"`
}

// PlatformAction represents a single action from a platform
type PlatformAction struct {
	ActionName string `json:"action_name"`
	Platform   string `json:"platform"`
}

// GetAllPlatformActions fetches all available platform actions from Composio MCP service
func (s *ComposioService) GetAllPlatformActions(ctx context.Context) (map[string][]string, error) {
	logger.Base().Info("Fetching all platform actions from Composio MCP service (base_url: )", zap.String("baseurl", s.BaseURL))

	url := fmt.Sprintf("%s/mcp/info", s.BaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Base().Error("Failed to create HTTP request")
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Base().Error("Failed to call Composio MCP service: (url: )", zap.String("url", url))
		return nil, fmt.Errorf("failed to call Composio MCP service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Base().Error("Composio MCP service returned non-200 status", zap.Int("status_code", resp.StatusCode), zap.String("response", string(bodyBytes)))
		return nil, fmt.Errorf("composio MCP service returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Base().Error("Failed to read response body")
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response PlatformActionsResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		logger.Base().Error("Failed to parse Composio MCP response", zap.String("body", string(bodyBytes)), zap.Error(err))
		return nil, fmt.Errorf("failed to parse Composio MCP response: %w", err)
	}

	logger.Base().Info("Successfully fetched platform actions from Composio MCP service", zap.Int("platform_count", len(response.PlatformTools)))

	return response.PlatformTools, nil
}

// GetPlatformActions fetches actions for specific platforms from Composio MCP service
func (s *ComposioService) GetPlatformActions(ctx context.Context, platforms []string) (map[string][]string, error) {
	logger.Base().Info("Fetching platform actions for specific platforms (base_url: , platforms: )", zap.String("baseurl", s.BaseURL))

	// Get all actions first
	allActions, err := s.GetAllPlatformActions(ctx)
	if err != nil {
		return nil, err
	}

	// Filter for requested platforms
	result := make(map[string][]string)
	for _, platform := range platforms {
		if actions, exists := allActions[platform]; exists {
			result[platform] = actions
		} else {
			// Platform exists but no actions found
			result[platform] = []string{}
			logger.Base().Warn("No actions found for platform", zap.String("platform", platform))
		}
	}

	logger.Base().Info("Successfully filtered platform actions", zap.Int("platform_count", len(result)))

	return result, nil
}

// RegisterActionItem represents a single action in the register request
type RegisterActionItem struct {
	ActionID string `json:"action_id"`
	AtID     string `json:"at_id"`
}

// RegisterToolsRequest represents the request body for registering tools with an agent
type RegisterToolsRequest struct {
	AgentID  string               `json:"agent_id"`
	Mode     string               `json:"mode"` // "draft" or "published"
	Actions  []RegisterActionItem `json:"actions"`
	TenantID string               `json:"tenant_id"`
	Modality string               `json:"modality"`
}

// RegisterToolsResponse represents the response from tool registration
type RegisterToolsResponse struct {
	Success bool `json:"success"`
}

// RegisterTools registers MCP tools with an agent
func (s *ComposioService) RegisterTools(ctx context.Context, req RegisterToolsRequest) (*RegisterToolsResponse, error) {
	logger.Base().Info("Registering tools with agent in MCP service", zap.String("agent_id", req.AgentID), zap.String("mode", req.Mode), zap.String("tenant_id", req.TenantID), zap.Int("actions_count", len(req.Actions)), zap.String("modality", req.Modality))

	url := fmt.Sprintf("%s/mcp/register", s.BaseURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		logger.Base().Error("Failed to marshal register tools request")
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		logger.Base().Error("Failed to create HTTP request")
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		logger.Base().Error("Failed to call MCP service: (url: )", zap.String("url", url))
		return nil, fmt.Errorf("failed to call MCP service: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Base().Error("Failed to read response body")
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logger.Base().Error("MCP service returned non-success status", zap.Int("status_code", resp.StatusCode), zap.String("response", string(bodyBytes)))
		return nil, fmt.Errorf("MCP service returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var response RegisterToolsResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		logger.Base().Error("Failed to parse register tools response")
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	logger.Base().Info("Tools registration completed", zap.String("agent_id", req.AgentID), zap.String("mode", req.Mode), zap.Bool("success", response.Success))

	return &response, nil
}

// MCPActionResponse represents the response from /mcp/actions/{action_id} endpoint
type MCPActionResponse struct {
	ActionName   string                 `json:"action_name"`
	ActionIntent string                 `json:"action_intent,omitempty"`
	AtID         string                 `json:"at_id"`
	AtName       string                 `json:"at_name"`
	AtType       string                 `json:"at_type"`
	McpTool      string                 `json:"mcp_tool,omitempty"`
	Platform     string                 `json:"platform,omitempty"`
	Status       string                 `json:"status"`
	TenantID     string                 `json:"tenant_id"`
	ConnectionID string                 `json:"connection_id,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedBy    string                 `json:"created_by"`
	UpdatedBy    string                 `json:"updated_by"`
}

// GetActionByID fetches a single action from MCP service by action ID
func (s *ComposioService) GetActionByID(ctx context.Context, actionID string) (*MCPActionResponse, error) {
	logger.Base().Info("Fetching action from MCP service (action_id: , base_url: )", zap.String("baseurl", s.BaseURL))

	url := fmt.Sprintf("%s/mcp/actions/%s", s.BaseURL, actionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Base().Error("Failed to create HTTP request")
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Base().Error("Failed to call MCP service: (url: )", zap.String("url", url))
		return nil, fmt.Errorf("failed to call MCP service: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Base().Error("Failed to read response body")
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logger.Base().Error("MCP service returned non-200 status", zap.Int("status_code", resp.StatusCode), zap.String("response", string(bodyBytes)))
		return nil, fmt.Errorf("MCP service returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var actionResponse MCPActionResponse
	if err := json.Unmarshal(bodyBytes, &actionResponse); err != nil {
		logger.Base().Error("Failed to parse MCP action response", zap.String("body", string(bodyBytes)), zap.Error(err))
		return nil, fmt.Errorf("failed to parse MCP action response: %w", err)
	}

	logger.Base().Info("Successfully fetched action from MCP service (action_id: , action_name: )", zap.String("actionname", actionResponse.ActionName))

	return &actionResponse, nil
}

// ActionTemplate represents the schema for action templates returned by /mcp/action_templates
type ActionTemplate struct {
	AtID              string   `json:"at_id"`
	AtName            string   `json:"at_name"`
	AtType            string   `json:"at_type"`
	McpTool           string   `json:"mcp_tool"`
	Platform          string   `json:"platform"`
	Status            string   `json:"status"`
	DisplayOrder      int      `json:"display_order"`
	NeedAuth          bool     `json:"need_auth"`
	AllowedAgentTypes []string `json:"allowed_agent_types"`
	AllowedModalities []string `json:"allowed_modalities"`
	Categories        []string `json:"categories"`
}

// GetActionTemplates fetches all available action templates from MCP service
func (s *ComposioService) GetActionTemplates(ctx context.Context) ([]ActionTemplate, error) {
	logger.Base().Info("Fetching action templates from MCP service (base_url: )", zap.String("baseurl", s.BaseURL))

	url := fmt.Sprintf("%s/mcp/action_templates", s.BaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Base().Error("Failed to create HTTP request")
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Use httpClient if available, otherwise create a new one
	client := s.httpClient
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Base().Error("Failed to call MCP service: (url: )", zap.String("url", url))
		return nil, fmt.Errorf("failed to call MCP service: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Base().Error("Failed to read response body")
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logger.Base().Error("MCP service returned non-200 status", zap.Int("status_code", resp.StatusCode), zap.String("response", string(bodyBytes)))
		return nil, fmt.Errorf("MCP service returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var actionTemplates []ActionTemplate
	if err := json.Unmarshal(bodyBytes, &actionTemplates); err != nil {
		logger.Base().Error("Failed to parse MCP action templates response", zap.String("body", string(bodyBytes)), zap.Error(err))
		return nil, fmt.Errorf("failed to parse MCP action templates response: %w", err)
	}

	logger.Base().Info("Successfully fetched action templates from MCP service", zap.Int("count", len(actionTemplates)))

	return actionTemplates, nil
}

// MCPTool represents a tool definition from MCP server
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	Annotations map[string]interface{} `json:"annotations,omitempty"`
}

// MCPJSONRPCRequest represents a JSON-RPC 2.0 request
type MCPJSONRPCRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// MCPJSONRPCResponse represents a JSON-RPC 2.0 response
type MCPJSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

// MCPError represents a JSON-RPC 2.0 error
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPToolsListResult represents the result of tools/list method
type MCPToolsListResult struct {
	Tools []MCPTool `json:"tools"`
}

// MCPToolCallResult represents the result of tools/call method
type MCPToolCallResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// MCPContent represents content in MCP responses
type MCPContent struct {
	Type string      `json:"type"`
	Text string      `json:"text,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

// CallTool calls an MCP tool endpoint
func (s *ComposioService) CallTool(ctx context.Context, endpoint string, args map[string]interface{}) (interface{}, error) {
	if s.BaseURL == "" {
		return nil, fmt.Errorf("MCP base URL not configured")
	}

	url := s.BaseURL + endpoint

	// Prepare request body
	reqBody, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Use httpClient if available, otherwise create a new one
	client := s.httpClient
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute MCP request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		logger.Base().Error("MCP tool call failed", zap.String("url", url), zap.Int("status_code", resp.StatusCode), zap.String("response", string(respBody)))
		return nil, fmt.Errorf("MCP request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var result interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		// If not JSON, return raw string
		return string(respBody), nil
	}

	return result, nil
}

// ListTools fetches all available tools from MCP server using JSON-RPC 2.0 protocol
// agentID is the agent identifier, mode should be "published" for production (active config) or "draft" for preview (draft config)
func (s *ComposioService) ListTools(ctx context.Context, agentID string, mode string, modality string) ([]MCPTool, error) {
	if s.BaseURL == "" {
		return nil, fmt.Errorf("MCP base URL not configured")
	}

	if agentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}

	if mode == "" {
		return nil, fmt.Errorf("mode is required")
	}

	// Default to voice modality if not specified
	if modality == "" {
		modality = ModalityVoice
	}

	// Build MCP server URL with agent_id, mode, and modality
	url := fmt.Sprintf("%s/mcp/server?agent_id=%s&mode=%s&modality=%s", s.BaseURL, agentID, mode, modality)

	logger.Base().Info("Fetching tools from MCP server (agent_id: , mode: , modality: )", zap.String("agent_id", agentID), zap.String("mode", mode), zap.String("modality", modality))

	// Prepare JSON-RPC 2.0 tools/list request
	mcpRequest := &MCPJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
		Params:  map[string]interface{}{},
	}

	// Execute the JSON-RPC call
	response, err := s.executeJSONRPC(ctx, url, mcpRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to execute MCP request: %w", err)
	}

	// Check for JSON-RPC error
	if response.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", response.Error.Code, response.Error.Message)
	}

	// Parse the result
	var result MCPToolsListResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools list result: %w", err)
	}

	logger.Base().Info("Fetched tools from MCP server", zap.String("agent_id", agentID), zap.Int("tools_count", len(result.Tools)))

	return result.Tools, nil
}

// CallToolMCP calls a specific tool on the MCP server using JSON-RPC 2.0 protocol
// agentID is the agent identifier, mode should be "published" for production (active config) or "draft" for preview (draft config)
func (s *ComposioService) CallToolMCP(ctx context.Context, agentID string, mode string, toolName string, arguments map[string]interface{}, modality string) (*MCPToolCallResult, error) {
	if s.BaseURL == "" {
		return nil, fmt.Errorf("MCP base URL not configured")
	}

	if agentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}

	if mode == "" {
		return nil, fmt.Errorf("mode is required")
	}

	// Default to voice modality if not specified
	if modality == "" {
		modality = ModalityVoice
	}

	// Build MCP server URL with agent_id, mode, and modality
	url := fmt.Sprintf("%s/mcp/server?agent_id=%s&mode=%s&modality=%s", s.BaseURL, agentID, mode, modality)

	logger.Base().Info("Calling MCP tool (agent_id: , mode: , modality: , tool_name: )", zap.String("agent_id", agentID), zap.String("mode", mode), zap.String("modality", modality), zap.String("toolname", toolName))

	// Prepare JSON-RPC 2.0 tools/call request
	mcpRequest := &MCPJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	// Execute the JSON-RPC call
	response, err := s.executeJSONRPC(ctx, url, mcpRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to execute MCP tool call: %w", err)
	}

	// Check for JSON-RPC error
	if response.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", response.Error.Code, response.Error.Message)
	}

	// Parse the result
	var result MCPToolCallResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool call result: %w", err)
	}

	return &result, nil
}

// executeJSONRPC executes a JSON-RPC 2.0 request and handles SSE response format
func (s *ComposioService) executeJSONRPC(ctx context.Context, url string, request *MCPJSONRPCRequest) (*MCPJSONRPCResponse, error) {
	// Marshal request
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON-RPC request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	// Use httpClient if available, otherwise create a new one
	client := s.httpClient
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		logger.Base().Error("HTTP request failed", zap.String("url", url), zap.Int("status_code", resp.StatusCode), zap.String("response", string(respBody)))
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Check if response is SSE format
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") || strings.Contains(contentType, "event-stream") {
		return s.parseSSEResponse(ctx, resp.Body)
	}

	// Otherwise, parse as regular JSON
	var response MCPJSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	return &response, nil
}

// parseSSEResponse parses Server-Sent Events (SSE) formatted response
func (s *ComposioService) parseSSEResponse(ctx context.Context, body io.Reader) (*MCPJSONRPCResponse, error) {
	scanner := bufio.NewScanner(body)
	var jsonData string

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "event: message" followed by "data: {json}"
		if strings.HasPrefix(line, "data: ") {
			jsonData = strings.TrimPrefix(line, "data: ")
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read SSE response: %w", err)
	}

	if jsonData == "" {
		return nil, fmt.Errorf("no data found in SSE response")
	}

	// Parse the JSON data
	var response MCPJSONRPCResponse
	if err := json.Unmarshal([]byte(jsonData), &response); err != nil {
		logger.Base().Error("Failed to parse SSE JSON data (json_data: , error: )", zap.String("jsondata", jsonData))
		return nil, fmt.Errorf("failed to parse SSE JSON data: %w", err)
	}

	return &response, nil
}
