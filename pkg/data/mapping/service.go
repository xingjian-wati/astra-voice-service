package mapping

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// AgentAttribute represents one attribute definition
type AgentAttribute struct {
	Label       string `json:"label"`
	DataType    string `json:"data_type"`
	Description string `json:"description"`
	Source      string `json:"source"`
	AttributeID string `json:"attribute_id"`
}

// AgentAttributes is a slice wrapper to implement DB JSON (Value/Scan) if needed via GORM
type AgentAttributes []AgentAttribute

// TemplateV2 represents the template structure
type TemplateV2 struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Icon              string                 `json:"icon"`
	Description       string                 `json:"description"`
	Type              string                 `json:"type"` // 'faq' | 'leadq'
	Category          string                 `json:"category"`
	Tags              []string               `json:"tags"`
	IntegratedActions []mcp.IntegratedAction `json:"integrated_actions"`
	Attributes        AgentAttributes        `json:"attributes,omitempty"`
	CommunicationMode string                 `json:"communication_mode"`
	TriggerType       string                 `json:"trigger_type"`
	Instructions      string                 `json:"instructions"`
	VoiceInstructions string                 `json:"voice_instructions"`
	SystemRules       string                 `json:"system_rules"`
	UsageCount        int64                  `json:"usage_count"`
}

// MappingService handles interactions with the mapping API
type MappingService struct {
	baseURL string
	client  *http.Client
}

// NewMappingService creates a new MappingService
func NewMappingService(baseURL string) *MappingService {
	return &MappingService{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetTemplate retrieves a template by ID
// It calls GET /mapping/v2/agents/templates/:id
func (s *MappingService) GetTemplate(id string) (*TemplateV2, error) {
	url := fmt.Sprintf("%s/mapping/v2/agents/templates/%s", s.baseURL, id)

	resp, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Base().Error("Mapping API error", zap.Int("status_code", resp.StatusCode), zap.String("body", string(bodyBytes)))
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var template TemplateV2
	if err := json.NewDecoder(resp.Body).Decode(&template); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &template, nil
}

// TextAgentConfig represents a minimal text agent configuration with only essential fields
type TextAgentConfig struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
	TemplateID   string `json:"template_id,omitempty"`
}

// GetTextAgent retrieves a text agent by ID with only essential fields
// It calls GET /mapping/v2/agents/:id and only parses the required fields
func (s *MappingService) GetTextAgent(id string) (*TextAgentConfig, error) {
	url := fmt.Sprintf("%s/mapping/v2/agents/%s", s.baseURL, id)

	resp, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Base().Error("Mapping API error", zap.Int("status_code", resp.StatusCode), zap.String("body", string(bodyBytes)))
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read the full response to parse only needed fields
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse JSON to extract only the fields we need
	var rawData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawData); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	config := &TextAgentConfig{
		ID:           getStringValue(rawData, "id"),
		Name:         getStringValue(rawData, "name"),
		Instructions: getStringValue(rawData, "instructions"),
		TemplateID:   getStringValue(rawData, "template_id"),
	}

	return config, nil
}

// getStringValue safely extracts a string value from a map
func getStringValue(data map[string]interface{}, key string) string {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}
