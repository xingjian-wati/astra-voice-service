package config

// WhatsAppCallConfig represents configuration for WhatsApp Call Gateway
type WhatsAppCallConfig struct {
	Port string

	// OpenAI configuration (reused from mediagateway)
	OpenAIAPIKey  string
	OpenAIBaseURL string

	// Gemini configuration
	GeminiAPIKey  string
	GeminiBaseURL string
	GeminiModel   string

	// WebRTC configuration
	STUNServers []string

	// Twilio Network Traversal Service (dynamic TURN credentials)
	TwilioAccountSID string
	TwilioAuthToken  string
	
	// Instance identifier for multi-pod monitoring and routing
	InstanceID string

	// Audio configuration
	AudioCodec     string
	MaxConnections int
	EnableCORS     bool

	// Wati configuration
	WatiBaseURL       string
	WatiTenantID      string
	WatiAPIKey        string
	WatiWebhookSecret string

	// Outbound call configuration
	OutboundBaseURL string

	// Audio Storage configuration
	AudioStorageEnabled bool
	AudioStorageType    string // "local" or "gcs"
	AudioStoragePath    string // Local directory path or GCS bucket name (based on AudioStorageType)

	// LiveKit configuration (NEW - for LiveKit integration)
	LiveKitEnabled   bool   // Whether LiveKit integration is enabled
	LiveKitServerURL string // LiveKit server WebSocket URL
	LiveKitAPIKey    string // LiveKit API key
	LiveKitAPISecret string // LiveKit API secret
	LiveKitGCSBucket string // GCS bucket for egress recordings (GKE auto-configured)
}
