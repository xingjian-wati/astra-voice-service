package config

import (
	"os"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// APIServiceConfig holds the API service configuration
type APIServiceConfig struct {
	APIServiceURL string
}

// DefaultAPIServiceConfig holds the default API service configuration values
var DefaultAPIServiceConfig = APIServiceConfig{
	APIServiceURL: "http://localhost:8001",
}

// LoadAPIServiceConfig loads API service configuration from environment variables
func LoadAPIServiceConfig() APIServiceConfig {
	config := DefaultAPIServiceConfig

	// Try ASTRA prefixed endpoint first (from .env)
	if endpoint := os.Getenv("ASTRA_API_SERVICE_GRPC_ENDPOINT"); endpoint != "" {
		config.APIServiceURL = endpoint
		return config
	}

	// Try ASTRA prefixed internal endpoint
	if endpoint := os.Getenv("ASTRA_API_SERVICE_INTERNAL_ENDPOINT"); endpoint != "" {
		config.APIServiceURL = endpoint
		return config
	}

	// Fallback to standard GRPC endpoint
	if endpoint := os.Getenv("API_SERVICE_GRPC_ENDPOINT"); endpoint != "" {
		config.APIServiceURL = endpoint
		return config
	}

	// Fallback to standard URL
	if endpoint := os.Getenv("API_SERVICE_URL"); endpoint != "" {
		config.APIServiceURL = endpoint
		return config
	}

	logger.Base().Warn("No API service endpoint found in environment, using default", zap.String("url", config.APIServiceURL))
	return config
}
