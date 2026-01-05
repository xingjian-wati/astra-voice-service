package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/handler"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

// Server represents the WhatsApp Call Gateway server
type Server struct {
	config         *config.WhatsAppCallConfig
	router         *mux.Router
	handlerManager *handler.HandlerManager
}

// NewServer creates a new WhatsApp Call Gateway server
func NewServer(cfg *config.WhatsAppCallConfig) *Server {
	// Initialize zap logger and redirect stdlib log to it
	if _, err := logger.Init(os.Getenv("LOG_ENV")); err != nil {
		logger.Base().Error("Failed to initialize zap logger, falling back to std log")
	}

	// Create router
	router := mux.NewRouter()

	// Initialize handler manager - it will create all services internally
	handlerManager, err := handler.NewHandlerManager(cfg)
	if err != nil {
		logger.Base().Error("Failed to initialize handler manager", zap.Error(err))
		return nil
	}

	// Setup all routes through handler manager
	handlerManager.SetupAllRoutes(router)

	// Create the server instance
	server := &Server{
		config:         cfg,
		router:         router,
		handlerManager: handlerManager,
	}

	return server
}

// Start starts the WhatsApp Call Gateway server
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%s", s.config.Port)

	server := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Base().Info("Starting server", zap.String("addr", addr))
	return server.ListenAndServe()
}

// LoadConfigFromEnv loads WhatsApp Call Gateway configuration from environment
func LoadConfigFromEnv() *config.WhatsAppCallConfig {
	// Load base config
	config.LoadConfig()

	cfg := &config.WhatsAppCallConfig{
		Port: getEnvOrDefault("WHATSAPP_CALL_PORT", "8082"),

		// OpenAI configuration
		OpenAIAPIKey:  getEnvOrDefault("OPENAI_API_KEY", ""),
		OpenAIBaseURL: getEnvOrDefault("OPENAI_BASE_URL", "https://api.openai.com"),

		// Gemini configuration (NEW)
		GeminiAPIKey:  getEnvOrDefault("GEMINI_API_KEY", ""),
		GeminiBaseURL: getEnvOrDefault("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),
		GeminiModel:   getEnvOrDefault("GEMINI_MODEL", "models/gemini-3-flash"),

		// WebRTC configuration - default STUN servers
		STUNServers: []string{
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
		},

		// Twilio Network Traversal Service (dynamic TURN credentials)
		TwilioAccountSID: getEnvOrDefault("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:  getEnvOrDefault("TWILIO_AUTH_TOKEN", ""),

		// Instance identifier for multi-pod monitoring and routing
		InstanceID: getDynamicInstanceID(),

		// Audio configuration
		AudioCodec:     getEnvOrDefault("WHATSAPP_AUDIO_CODEC", "g711_ulaw"),
		MaxConnections: getEnvAsIntOrDefault("WHATSAPP_MAX_CONNECTIONS", 50),
		EnableCORS:     getEnvAsBoolOrDefault("WHATSAPP_ENABLE_CORS", true),

		// Wati configuration
		WatiBaseURL:       getEnvOrDefault("WATI_BASE_URL", "https://live-server-113033.wati.io"),
		WatiTenantID:      getEnvOrDefault("WATI_TENANT_ID", ""),
		WatiAPIKey:        getEnvOrDefault("WATI_API_KEY", ""),
		WatiWebhookSecret: getEnvOrDefault("WATI_WEBHOOK_SECRET", ""),

		// Outbound call configuration
		OutboundBaseURL: getEnvOrDefault("OUTBOUND_BASE_URL", ""),

		// Audio Storage configuration
		AudioStorageEnabled: getEnvAsBoolOrDefault("AUDIO_STORAGE_ENABLED", false),
		AudioStorageType:    getEnvOrDefault("AUDIO_STORAGE_TYPE", "gcs"),
		AudioStoragePath:    getEnvOrDefault("AUDIO_STORAGE_PATH", ""),

		// LiveKit configuration (NEW)
		LiveKitEnabled:   getEnvAsBoolOrDefault("LIVEKIT_ENABLED", false),
		LiveKitServerURL: getEnvOrDefault("LIVEKIT_SERVER_URL", ""),
		LiveKitAPIKey:    getEnvOrDefault("LIVEKIT_API_KEY", ""),
		LiveKitAPISecret: getEnvOrDefault("LIVEKIT_API_SECRET", ""),
		LiveKitGCSBucket: getEnvOrDefault("LIVEKIT_GCS_BUCKET", ""), // GCS bucket for egress (GKE auto-configured)
	}

	// Load custom STUN servers from environment if provided
	if stunServers := os.Getenv("WHATSAPP_STUN_SERVERS"); stunServers != "" {
		cfg.STUNServers = splitAndTrimStrings(stunServers, ",")
		logger.Base().Info("Using custom STUN servers", zap.Strings("stun_servers", cfg.STUNServers))
	}

	return cfg
}

// getEnvOrDefault gets environment variable or returns default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsIntOrDefault gets environment variable as int or returns default
func getEnvAsIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsBoolOrDefault gets environment variable as bool or returns default
func getEnvAsBoolOrDefault(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// splitAndTrimStrings splits a string by delimiter and trims whitespace from each part
func splitAndTrimStrings(s, delimiter string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, delimiter)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// getDynamicInstanceID generates a unique identifier for this service instance.
// It first tries to use the system hostname (pod name in K8s),
// then falls back to an environment variable, and finally a timestamp-based random ID.
func getDynamicInstanceID() string {

	// 1. Try system hostname (Pod Name in Kubernetes)
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}

	// 2. Fallback to timestamp based ID
	return fmt.Sprintf("voice-service-%d", time.Now().UnixNano())
}

// GetConnectionCount returns the current number of active connections
func (s *Server) GetConnectionCount() int {
	return s.handlerManager.GetService().GetConnectionCount()
}

func main() {
	// 0. Load .env file for local development if it exists
	// This will not override environment variables set by Helm/Docker
	if err := godotenv.Load(); err != nil {
		log.Printf("Info: .env file not found or skipped (expected in production): %v", err)
	}

	// 1. Load configuration from environment
	cfg := LoadConfigFromEnv()
	fmt.Printf("🚀 Starting Astra Voice Service (Instance: %s)\n", cfg.InstanceID)

	// 2. Create the server
	server := NewServer(cfg)
	if server == nil {
		log.Fatal("❌ Failed to create server")
	}
	logger.Base().Info("✅ Server initialized successfully",
		zap.String("port", cfg.Port),
		zap.String("instance_id", cfg.InstanceID))

	// 3. Start the server
	if err := server.Start(); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}
