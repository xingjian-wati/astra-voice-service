package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// WebRTC Constants
	DefaultSTUNServer1 = "stun:stun.l.google.com:19302"
	DefaultSTUNServer2 = "stun:stun1.l.google.com:19302"

	// Audio Constants
	DefaultSampleRate     = 48000
	DefaultChannelsMono   = 1
	DefaultChannelsStereo = 2
	DefaultFrameDuration  = 20 * time.Millisecond
	DefaultOpusBitrate    = 32000

	// Connection Constants
	DefaultConnectionTimeout = 30 * time.Second

	// Default Settings
	DefaultLanguage = "en"

	// Identifier Constants
	DefaultLiveKitBotName   = "livekit-bot"
	DefaultLiveKitBotPrefix = "bot-"
	DefaultRoomPrefix       = "astra-"
	ParticipantPrefixFilter = "hamming"

	// Egress Constants
	DefaultEgressPathPrefix = "livekit_dev/"
	DefaultEgressExtension  = ".ogg"

	// Event Constants
	EventWhatsAppAudioReady = "whatsapp_audio_ready"
)

// WebSocketConfig holds configuration for WebSocket service
type WebSocketConfig struct {
	// Twilio configuration
	TwilioAccountSID string
	TwilioAuthToken  string
	TwilioAppSID     string

	// OpenAI configuration
	OpenAIAPIKey  string
	OpenAIBaseURL string

	// Gemini configuration
	GeminiAPIKey  string
	GeminiBaseURL string
	GeminiModel   string

	// WebSocket configuration
	STUNServers    []string
	TURNServers    []string
	AudioCodec     string
	MaxConnections int

	// Server configuration
	Port       string
	EnableCORS bool
}

// LoadWebSocketConfig loads WebSocket configuration from environment variables
func LoadWebSocketConfig() *WebSocketConfig {
	config := &WebSocketConfig{
		// Twilio defaults
		TwilioAccountSID: getEnv("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:  getEnv("TWILIO_AUTH_TOKEN", ""),
		TwilioAppSID:     getEnv("TWILIO_APP_SID", ""),

		// OpenAI defaults
		OpenAIAPIKey:  getEnv("OPENAI_API_KEY", ""),
		OpenAIBaseURL: getEnv("OPENAI_BASE_URL", "https://api.openai.com"),

		// Gemini defaults
		GeminiAPIKey:  getEnv("GEMINI_API_KEY", ""),
		GeminiBaseURL: getEnv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),
		GeminiModel:   getEnv("GEMINI_MODEL", "models/gemini-3-flash"),

		// WebSocket defaults
		STUNServers: []string{
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
		},
		TURNServers:    []string{},
		AudioCodec:     "opus",
		MaxConnections: getEnvAsInt("WEBSOCKET_MAX_CONNECTIONS", 100),

		// Server defaults
		Port:       getEnv("WEBSOCKET_PORT", "8081"),
		EnableCORS: getEnvAsBool("WEBSOCKET_ENABLE_CORS", true),
	}

	// Load custom STUN servers if provided
	if stunServers := os.Getenv("WEBSOCKET_STUN_SERVERS"); stunServers != "" {
		config.STUNServers = splitString(stunServers, ",")
	}

	// Load custom TURN servers if provided
	if turnServers := os.Getenv("WEBSOCKET_TURN_SERVERS"); turnServers != "" {
		config.TURNServers = splitString(turnServers, ",")
	}

	return config
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt gets an environment variable as an integer with a default value
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsBool gets an environment variable as a boolean with a default value
func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// splitString splits a string by delimiter and trims whitespace
func splitString(s, delimiter string) []string {
	parts := strings.Split(s, delimiter)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
