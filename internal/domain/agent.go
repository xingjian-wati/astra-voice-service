package domain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
)

// VoiceAgent represents an AI agent configuration for a tenant
type VoiceAgent struct {
	ID                   string           `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	VoiceTenantID        string           `json:"voice_tenant_id" gorm:"type:varchar(255);not null;index;uniqueIndex:idx_tenant_text_agent"`
	VoiceTenant          VoiceTenant      `json:"voice_tenant" gorm:"foreignKey:VoiceTenantID;references:tenant_id"`
	AgentName            string           `json:"agent_name" gorm:"type:varchar(255);not null"`
	TextAgentID          *string          `json:"text_agent_id" gorm:"type:varchar(255);uniqueIndex:idx_tenant_text_agent"`
	Instruction          *string          `json:"instruction" gorm:"type:text"`
	AgentConfig          *AgentConfigData `json:"agent_config" gorm:"type:jsonb"`           // Draft configuration
	PublishedAgentConfig *AgentConfigData `json:"published_agent_config" gorm:"type:jsonb"` // Published configuration
	CreatedAt            time.Time        `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt            time.Time        `json:"updated_at" gorm:"autoUpdateTime"`
	Disabled             bool             `json:"disabled" gorm:"default:false"`
}

// TableName sets the table name for VoiceAgent
func (VoiceAgent) TableName() string {
	return "voice_agents_config"
}

// CreateVoiceAgentRequest represents the request to create a new voice agent
type CreateVoiceAgentRequest struct {
	VoiceTenantID string           `json:"voice_tenant_id" validate:"required"`
	AgentName     string           `json:"agent_name" validate:"required"`
	TextAgentID   *string          `json:"text_agent_id,omitempty"`
	Instruction   *string          `json:"instruction,omitempty"`
	AgentConfig   *AgentConfigData `json:"agent_config,omitempty"`
}

// UpdateVoiceAgentRequest represents the request to update a voice agent
type UpdateVoiceAgentRequest struct {
	AgentName   *string          `json:"agent_name,omitempty"`
	Instruction *string          `json:"instruction,omitempty"`
	AgentConfig *AgentConfigData `json:"agent_config,omitempty"`
	Disabled    *bool            `json:"disabled,omitempty"`
}

// PublishAgentRequest represents the request to publish an agent's configuration
type PublishAgentRequest struct {
	AgentID string `json:"agent_id" validate:"required"` // The ID of the agent to publish
	Comment string `json:"comment,omitempty"`            // Optional comment for the publication log
}

// PublishAgentResponse represents the response after publishing an agent
type PublishAgentResponse struct {
	ID          string    `json:"id"`
	PublishedAt time.Time `json:"published_at"`
}

// QuickCreateAgentRequest represents the request to quickly create an agent from brandkit
type QuickCreateAgentRequest struct {
	TenantID    string `json:"tenant_id" validate:"required"`
	TextAgentID string `json:"text_agent_id" validate:"required"`
	TemplateID  string `json:"template_id" validate:"required"`
}

// AgentConfigData represents the structured agent configuration data
// This mirrors the config.AgentConfig but is designed for database storage
type AgentConfigData struct {
	// Basic Agent Information
	BusinessNumber string `json:"business_number,omitempty"`

	// Agent Personality & Behavior
	Persona       string   `json:"persona,omitempty"`
	Services      []string `json:"services,omitempty"`
	Tone          string   `json:"tone,omitempty"`           // e.g., "professional", "friendly", "casual"
	Language      string   `json:"language,omitempty"`       // primary language
	DefaultAccent string   `json:"default_accent,omitempty"` // Default accent for primary language
	Expertise     []string `json:"expertise,omitempty"`

	// Voice Configuration
	Voice string  `json:"voice,omitempty"` // OpenAI voice: alloy, echo, fable, onyx, nova, shimmer
	Speed float64 `json:"speed,omitempty"` // Speech speed: 0.25 to 4.0 (default: 1.0)

	// Call Configuration
	MaxCallDuration int                `json:"max_call_duration,omitempty"` // in seconds
	SilenceConfig   *SilenceConfigData `json:"silence_config,omitempty"`

	// Prompt Configuration
	PromptConfig *PromptConfigData `json:"prompt_config,omitempty"`
	// Integrated Actions Configuration
	IntegratedActions []mcp.IntegratedAction `json:"integrated_actions,omitempty"`

	// Outbound Call Configuration
	OutboundPromptConfig      *PromptConfigData      `json:"outbound_prompt_config,omitempty"`
	OutboundIntegratedActions []mcp.IntegratedAction `json:"outbound_integrated_actions,omitempty"`

	// RAG Configuration
	RAGConfig *RAGConfigData `json:"rag_config,omitempty"`

	// API & Integration Configuration
	APIConfig *APIConfigData `json:"api_config,omitempty"`

	// Business Logic Configuration
	BusinessRules *BusinessRulesData `json:"business_rules,omitempty"`
}

// PromptConfigData contains prompt-related configuration
type PromptConfigData struct {
	GreetingTemplate     string            `json:"greeting_template,omitempty"`
	RealtimeTemplate     string            `json:"realtime_template,omitempty"`
	SystemInstructions   string            `json:"system_instructions,omitempty"`
	ConversationFlow     []string          `json:"conversation_flow,omitempty"`
	ExampleDialogues     map[string]string `json:"example_dialogues,omitempty"`
	LanguageInstructions map[string]string `json:"language_instructions,omitempty"` // Accent configuration per language, e.g., {"en": "india", "zh": "mainland"}
	CustomVariables      map[string]string `json:"custom_variables,omitempty"`

	// Language & Accent Adaptation Settings (default: true)
	AutoLanguageSwitching *bool `json:"auto_language_switching,omitempty"` // Enable automatic language switching based on caller's language
	AutoAccentAdaptation  *bool `json:"auto_accent_adaptation,omitempty"`  // Enable automatic accent adaptation based on caller's region
}

// RAGConfigData contains RAG-specific configuration
type RAGConfigData struct {
	Enabled     bool              `json:"enabled,omitempty"`
	BaseURL     string            `json:"base_url,omitempty"`
	Token       string            `json:"token,omitempty"`
	WorkflowID  string            `json:"workflow_id,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Description string            `json:"description,omitempty"`
	Timeout     int               `json:"timeout,omitempty"` // in seconds
	MaxRetries  int               `json:"max_retries,omitempty"`
}

// APIConfigData contains API endpoints and configurations
type APIConfigData struct {
	Endpoints map[string]string `json:"endpoints,omitempty"`
	Tokens    map[string]string `json:"tokens,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// BusinessRulesData contains business-specific rules and configurations
type BusinessRulesData struct {
	AllowedActions      []string                     `json:"allowed_actions,omitempty"`
	RequiredFields      []string                     `json:"required_fields,omitempty"`
	ValidationRules     map[string]string            `json:"validation_rules,omitempty"`
	WorkingHours        *WorkingHoursData            `json:"working_hours,omitempty"`
	EscalationRules     []EscalationRuleData         `json:"escalation_rules,omitempty"`
	MaxConversationTime int                          `json:"max_conversation_time,omitempty"` // in minutes
	FunctionCallRules   map[string]*FunctionRuleData `json:"function_call_rules,omitempty"`   // Detailed rules for each function
}

// WorkingHoursData defines when the agent is available
type WorkingHoursData struct {
	Timezone string            `json:"timezone,omitempty"`
	Schedule map[string]string `json:"schedule,omitempty"` // day -> "09:00-17:00"
}

// EscalationRuleData defines when and how to escalate conversations
type EscalationRuleData struct {
	Condition string `json:"condition,omitempty"`
	Action    string `json:"action,omitempty"`
	Target    string `json:"target,omitempty"`
}

// FunctionRuleData defines simplified rules for when and how to call a specific function
type FunctionRuleData struct {
	Description string                 `json:"description,omitempty"` // What this function does and when to use it
	When        string                 `json:"when,omitempty"`        // Simple trigger condition
	Parameters  map[string]interface{} `json:"parameters,omitempty"`  // OpenAI function parameters schema
}

// SilenceConfigData contains configuration for handling silence/inactivity
type SilenceConfigData struct {
	InactivityCheckDuration int    `json:"inactivity_check_duration,omitempty"` // seconds
	MaxRetries              int    `json:"max_retries,omitempty"`
	InactivityMessage       string `json:"inactivity_message,omitempty"`
}

// Implement driver.Valuer interface for AgentConfigData
func (a AgentConfigData) Value() (driver.Value, error) {
	// Check if the struct is empty by marshaling and checking if it's just "{}"
	data, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	if string(data) == "{}" {
		return nil, nil
	}
	return data, nil
}

// Implement sql.Scanner interface for AgentConfigData
func (a *AgentConfigData) Scan(value interface{}) error {
	if value == nil {
		*a = AgentConfigData{}
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into AgentConfigData", value)
	}

	return json.Unmarshal(bytes, a)
}

// PlatformVoiceAgent represents the voice_agents table (platform side)
// This is different from voice_agents_config table
type PlatformVoiceAgent struct {
	ID                     string    `json:"id" gorm:"type:varchar(255);primary_key"`
	PlatformAgentID        string    `json:"platform_agent_id" gorm:"type:varchar(255);not null;index"`
	IsEnabled              bool      `json:"is_enabled" gorm:"default:true"`
	VoiceModeID            *string   `json:"voice_mode_id" gorm:"type:varchar(255)"`
	Language               *string   `json:"language" gorm:"type:varchar(50)"`
	Strategy               *string   `json:"strategy" gorm:"type:jsonb"`
	MaximumSessionTime     int64     `json:"maximum_session_time" gorm:"default:0"`
	InactivityCheckDelay   int64     `json:"inactivity_check_delay" gorm:"default:0"`
	MaxRetries             int64     `json:"max_retries" gorm:"default:3"`
	InactivityCheckMessage *string   `json:"inactivity_check_message" gorm:"type:text"`
	IsDeleted              bool      `json:"is_deleted" gorm:"default:false"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
	PathwayID              *string   `json:"pathway_id" gorm:"type:varchar(255)"`
	TenantID               string    `json:"tenant_id" gorm:"type:varchar(255);not null;index"`
	AgentAPIKey            string    `json:"agent_api_key" gorm:"type:varchar(255);default:''"`
	Environment            string    `json:"environment" gorm:"type:varchar(50);default:'live';not null;index"`
}

// TableName sets the table name for PlatformVoiceAgent
func (PlatformVoiceAgent) TableName() string {
	return "voice_agents"
}

// DefaultTemplateFactories is a map of functions that return default agent configurations by category
var DefaultTemplateFactories = map[string]func() *AgentConfigData{
	"default": createLeadQualificationConfig,
}

// GetDefaultAgentConfig returns the default configuration for a voice agent
// This is used as a fallback when brandkit data is not available
func GetDefaultAgentConfig() *AgentConfigData {
	return GetDefaultConfigByCategory("default")
}

// GetDefaultConfigByCategory returns the default configuration for a specific category
func GetDefaultConfigByCategory(category string) *AgentConfigData {
	if factory, ok := DefaultTemplateFactories[category]; ok {
		return factory()
	}
	// Fallback to default if category not found
	return DefaultTemplateFactories["default"]()
}

// createLeadQualificationConfig returns the default lead qualification configuration
func createLeadQualificationConfig() *AgentConfigData {
	// Shared RealtimeTemplate for both inbound and outbound
	sharedRealtimeTemplate := `## Agent Role

You are a trained voice-based assistant designed to guide callers through Lead Qualification and meeting scheduling, while maintaining natural, concise, and accurate spoken communication.

## Core Objectives

- **Collect essential qualification information step by step.**

- **Provide answers only using the supplied knowledge content.**

- **Avoid hallucinations and never invent links, prices, features, or policies.**

- **Suggest or complete a meeting booking when appropriate.**

- **Maintain smooth and human-like voice conversation flow.**

## Guardrails

**Knowledge Boundaries**

- Use only the knowledge provided by the system.

- Do not generate any information not explicitly included.

- If asked about unsupported topics, politely redirect back to qualification.

**Behavioral Boundaries**

- Do not over-greet or use generic phrases like "Hello there."

- Ask one question at a time.

- Never interrupt the caller; pause briefly before speaking.

- Keep responses concise and spoken naturally, not like a script.

## Lead Qualification Flow

Execute in this priority order:

**1. Meeting Action (Highest Priority)**

- If the caller requests a demo, meeting, or callback → complete the booking.

- If the caller qualifies as a strong lead → proactively offer a meeting.

**2. Qualification Questions**

- Ask one question at a time, based on caller responses:

	•	Current business size

	•	Type of business or role

	•	Problems they want to solve

	•	Expected timeline

	•	Tools or solutions they currently use

- Avoid repeating questions if the information is already known.

**3. Knowledge-Based Q&A**

- If the answer exists in the knowledge content → answer briefly.

- If not → decline politely and return to qualification or meeting setup.

**4. Fallback**

- **When the caller goes off-track:**

	•	Redirect gently

	•	Maintain progress toward qualification or booking

- **If the caller goes off-track:**

	•	Redirect gently

	•	Maintain progress toward qualification or booking

**5. Conversational Style**

	•	Natural, friendly, and efficient.

	•	Short sentences with clear voice rhythm.

	•	Avoid dense info dumps—pace content like a real human agent.

	•	No Markdown formatting in spoken output.

**6. Micro-Guidelines**

	•	Acknowledge understanding: "Got it," "Makes sense," "Understood."

	•	Confirm key details before moving on.

	•	If the caller hesitates: gently guide them.

	•	If asked why certain details are needed:

"This helps me understand whether we can offer the right solution."

**7. Error & Boundary Handling**

	•	If audio is unclear: "Sorry, I didn't catch that. Could you say it again?"

	•	If the caller refuses a question:

"No problem, we can continue with something else."

	•	Maintain overall goal even if the order changes.

**8. Mini Version (<30 tokens)**

Voice-optimized LeadQ agent: stepwise qualification, knowledge-bound, no hallucinations, concise replies, meeting booking prioritized.`

	return &AgentConfigData{
		Persona:       "Professional Voice Assistant",
		Tone:          "professional, helpful, and concise",
		Language:      "en",
		Voice:         "alloy",
		Speed:         1.0,
		DefaultAccent: "us",
		Services:      []string{"lead_qualification", "meeting_booking", "q_and_a"},
		Expertise:     []string{"customer_service", "sales", "scheduling"},
		PromptConfig: &PromptConfigData{
			GreetingTemplate:   "Hello! I am your voice assistant. How can I help you today?",
			RealtimeTemplate:   sharedRealtimeTemplate,
			SystemInstructions: "You are a helpful assistant. Keep your responses short and concise for voice interaction.",
		},
		OutboundPromptConfig: &PromptConfigData{
			GreetingTemplate:   "Hello! This is {{.CompanyName}}. I'm calling to see how we can help you today.",
			RealtimeTemplate:   sharedRealtimeTemplate,
			SystemInstructions: "You are a helpful assistant. Keep your responses short and concise for voice interaction.",
		},
	}
}
