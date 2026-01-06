package config

import (
	"os"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
)

// DefaultTenantID falls back to "wati" but can be overridden via DEFAULT_ASTRA_TENANT_ID env var.
var DefaultTenantID = getDefaultTenantID()

// DefaultWatiTenantID falls back to "" but can be overridden via WATI_TENANT_ID env var.
var DefaultWatiTenantID = getDefaultWatiTenantID()

// RefreshDefaults re-loads default tenant IDs from environment variables.
// This should be called after loading .env files.
func RefreshDefaults() {
	DefaultTenantID = getDefaultTenantID()
	DefaultWatiTenantID = getDefaultWatiTenantID()
}

func getDefaultTenantID() string {
	if v := os.Getenv("DEFAULT_ASTRA_TENANT_ID"); v != "" {
		return v
	}
	return "wati"
}

func getDefaultWatiTenantID() string {
	if v := os.Getenv("WATI_TENANT_ID"); v != "" {
		return v
	}
	return ""
}

const (
	MessageRoleUser      = "user"
	MessageRoleAssistant = "assistant"
	MessageRoleSystem    = "system"
	MessageRoleFunction  = "function"
)

// ConversationMessage represents a conversation message
// This type is shared across packages to avoid circular dependencies
type ConversationMessage struct {
	ID        string    `json:"id,omitempty"` // Stable identifier for the message
	Role      string    `json:"role"`         // "user", "assistant", "system"
	Content   string    `json:"content"`      // The message content
	Timestamp time.Time `json:"timestamp"`    // When the message was created
}

const (
	AgentConfigModeDraft     = "draft"
	AgentConfigModePublished = "published"
)

const (
	DefaultSilenceInactivityCheckDuration = 20
	DefaultSilenceMaxRetries              = 5
	DefaultSilenceMessage                 = "Are you still there? I'm here to help if you need anything."
	DefaultConfidenceThreshold            = 75.0
)

// AgentConfig represents a complete agent configuration
type AgentConfig struct {
	// Basic Agent Information
	ID             string    `json:"id" db:"id"`
	TextAgentID    string    `json:"text_agent_id,omitempty" db:"text_agent_id"` // Text agent ID from platform
	Name           string    `json:"name" db:"name"`
	CompanyName    string    `json:"company_name" db:"company_name"`
	BusinessNumber string    `json:"business_number" db:"business_number"`
	Industry       string    `json:"industry" db:"industry"`
	Description    string    `json:"description" db:"description"`
	IsActive       bool      `json:"is_active" db:"is_active"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`

	// Agent Personality & Behavior
	Persona       string   `json:"persona" db:"persona"`
	Services      []string `json:"services" db:"services"`
	Tone          string   `json:"tone" db:"tone"`                     // e.g., "professional", "friendly", "casual"
	Language      string   `json:"language" db:"language"`             // primary language
	DefaultAccent string   `json:"default_accent" db:"default_accent"` // Default accent for primary language
	Expertise     []string `json:"expertise" db:"expertise"`

	// Voice Configuration
	Voice string  `json:"voice" db:"voice"` // OpenAI voice: alloy, echo, fable, onyx, nova, shimmer
	Speed float64 `json:"speed" db:"speed"` // Speech speed: 0.25 to 4.0 (default: 1.0)

	// Call Configuration
	MaxCallDuration int            `json:"max_call_duration" db:"max_call_duration"` // in seconds
	SilenceConfig   *SilenceConfig `json:"silence_config" db:"silence_config"`

	// Prompt Configuration
	PromptConfig *PromptConfig `json:"prompt_config"`

	// RAG Configuration
	RAGConfig *RAGConfig `json:"rag_config"`

	// API & Integration Configuration
	APIConfig *APIConfig `json:"api_config"`

	// Business Logic Configuration
	BusinessRules *BusinessRules `json:"business_rules"`

	// Integrated Actions Configuration
	IntegratedActions []mcp.IntegratedAction `json:"integrated_actions" db:"integrated_actions"`

	// Outbound Call Configuration
	OutboundPromptConfig      *PromptConfig          `json:"outbound_prompt_config"`
	OutboundIntegratedActions []mcp.IntegratedAction `json:"outbound_integrated_actions" db:"outbound_integrated_actions"`
}

// SilenceConfig contains configuration for handling silence/inactivity
type SilenceConfig struct {
	InactivityCheckDuration int    `json:"inactivity_check_duration" db:"inactivity_check_duration"` // seconds
	MaxRetries              int    `json:"max_retries" db:"max_retries"`
	InactivityMessage       string `json:"inactivity_message" db:"inactivity_message"`
}

// SetDefaults fills missing SilenceConfig fields with safe defaults.
func (s *SilenceConfig) SetDefaults() {
	if s == nil {
		return
	}
	if s.InactivityCheckDuration <= 0 {
		s.InactivityCheckDuration = DefaultSilenceInactivityCheckDuration
	}
	if s.MaxRetries <= 0 {
		s.MaxRetries = DefaultSilenceMaxRetries
	}
	if s.InactivityMessage == "" {
		s.InactivityMessage = DefaultSilenceMessage
	}
}

// PromptConfig contains all prompt-related configuration for an agent
type PromptConfig struct {
	GreetingTemplate     string            `json:"greeting_template" db:"greeting_template"`
	RealtimeTemplate     string            `json:"realtime_template" db:"realtime_template"`
	SystemInstructions   string            `json:"system_instructions" db:"system_instructions"`
	ConversationFlow     []string          `json:"conversation_flow" db:"conversation_flow"`
	ExampleDialogues     map[string]string `json:"example_dialogues" db:"example_dialogues"`
	LanguageInstructions map[string]string `json:"language_instructions" db:"language_instructions"` // Accent configuration per language, e.g., {"en": "india", "zh": "mainland"}
	CustomVariables      map[string]string `json:"custom_variables" db:"custom_variables"`

	// Language & Accent Adaptation Settings (default: true)
	AutoLanguageSwitching *bool `json:"auto_language_switching,omitempty"` // Enable automatic language switching based on caller's language
	AutoAccentAdaptation  *bool `json:"auto_accent_adaptation,omitempty"`  // Enable automatic accent adaptation based on caller's region
}

// RAGConfig contains RAG-specific configuration for each agent
type RAGConfig struct {
	Enabled     bool              `json:"enabled" db:"enabled"`
	BaseURL     string            `json:"base_url" db:"base_url"`
	Token       string            `json:"token" db:"token"`
	WorkflowID  string            `json:"workflow_id" db:"workflow_id"`
	Headers     map[string]string `json:"headers" db:"headers"`
	Description string            `json:"description" db:"description"`
	Timeout     int               `json:"timeout" db:"timeout"` // in seconds
	MaxRetries  int               `json:"max_retries" db:"max_retries"`
}

// APIConfig contains API endpoints and configurations for agent actions
type APIConfig struct {
	Endpoints map[string]string `json:"endpoints" db:"endpoints"`
	Tokens    map[string]string `json:"tokens" db:"tokens"`
	Headers   map[string]string `json:"headers" db:"headers"`
}

// BusinessRules contains business-specific rules and configurations
type BusinessRules struct {
	AllowedActions      []string                 `json:"allowed_actions" db:"allowed_actions"`
	RequiredFields      []string                 `json:"required_fields" db:"required_fields"`
	ValidationRules     map[string]string        `json:"validation_rules" db:"validation_rules"`
	WorkingHours        *WorkingHours            `json:"working_hours"`
	EscalationRules     []EscalationRule         `json:"escalation_rules"`
	MaxConversationTime int                      `json:"max_conversation_time"` // in minutes
	FunctionCallRules   map[string]*FunctionRule `json:"function_call_rules"`   // Detailed rules for each function
}

// WorkingHours defines when the agent is available
type WorkingHours struct {
	Timezone string            `json:"timezone" db:"timezone"`
	Schedule map[string]string `json:"schedule" db:"schedule"` // day -> "09:00-17:00"
}

// EscalationRule defines when and how to escalate conversations
type EscalationRule struct {
	Condition string `json:"condition" db:"condition"`
	Action    string `json:"action" db:"action"`
	Target    string `json:"target" db:"target"`
}

// FunctionRule defines simplified rules for when and how to call a specific function
// The function name is the map key in FunctionCallRules, so it's not duplicated here
type FunctionRule struct {
	Description string                 `json:"description"` // What this function does and when to use it (required)
	When        string                 `json:"when"`        // Simple trigger condition, e.g., "user wants to book a demo" (optional)
	Parameters  map[string]interface{} `json:"parameters"`  // OpenAI function parameters schema (optional, follows OpenAI format)
}

// AgentFetcher interface for agent data access
type AgentFetcher interface {
	GetAgent(id string) (*AgentConfig, error)
	GetAllAgents() ([]*AgentConfig, error)
	GetActiveAgents() ([]*AgentConfig, error)
	ListAgentIDs() ([]string, error)
}

// AgentRepository interface for agent data management (create, update, delete)
type AgentRepository interface {
	AgentFetcher
	CreateAgent(agent *AgentConfig) error
	UpdateAgent(agent *AgentConfig) error
	DeleteAgent(id string) error
}

// PromptGenerator interface for generating AI prompts based on agent configuration
type PromptGenerator interface {
	GenerateGreetingInstruction(contactName, contactNumber, language, accent string) string
	GenerateOutboundGreeting(contactName, contactNumber, language, accent string) string // Self-introduction greeting for outbound calls
	GenerateRealtimeInstruction(contactNumber string) string
	GenerateSessionInstructions(contactNumber, language, accent string, isOutbound bool) string // Complete session-level instructions (RealtimeTemplate, language, accent, contact, phone rules, functions)
	GenerateAccentInstruction() string
	GetAgentID() string
	GetAgentName() string
	GetCompanyName() string
}

// GetDefaultAgentID returns the default agent ID (for backward compatibility)
func GetDefaultAgentID() string {
	return "wati-sarah"
}

// GetAgentByBusinessType returns agent ID based on old business type (for migration)
func GetAgentByBusinessType(businessType string) string {
	switch businessType {
	case "wati":
		return "wati-sarah"
	case "car_dealer":
		return "nextgear-mike"
	case "financial":
		return "peak-capital-emma"
	default:
		return "wati-sarah" // default fallback
	}
}

// IsAutoLanguageSwitchingEnabled returns true if auto language switching is enabled (default: true)
func (p *PromptConfig) IsAutoLanguageSwitchingEnabled() bool {
	if p == nil || p.AutoLanguageSwitching == nil {
		return true // default is enabled
	}
	return *p.AutoLanguageSwitching
}

// IsAutoAccentAdaptationEnabled returns true if auto accent adaptation is enabled (default: true)
func (p *PromptConfig) IsAutoAccentAdaptationEnabled() bool {
	if p == nil || p.AutoAccentAdaptation == nil {
		return true // default is enabled
	}
	return *p.AutoAccentAdaptation
}
