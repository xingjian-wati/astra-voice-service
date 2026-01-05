package config

import (
	"os"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// RagAPIServiceConfig holds the RAG API service configuration
type RagAPIServiceConfig struct {
	RagAPIServiceURL string
}

// DefaultRagAPIServiceConfig holds the default RAG API service configuration values
var DefaultRagAPIServiceConfig = RagAPIServiceConfig{
	RagAPIServiceURL: "http://localhost:8000",
}

// LoadRagAPIServiceConfig loads RAG API service configuration from environment variables
func LoadRagAPIServiceConfig() RagAPIServiceConfig {
	config := DefaultRagAPIServiceConfig

	// Try ASTRA prefixed endpoint first (from .env)
	if endpoint := os.Getenv("ASTRA_RAG_API_SERVICE_GRPC_ENDPOINT"); endpoint != "" {
		config.RagAPIServiceURL = endpoint
		return config
	}

	// Try ASTRA prefixed internal endpoint
	if endpoint := os.Getenv("ASTRA_RAG_API_SERVICE_INTERNAL_ENDPOINT"); endpoint != "" {
		config.RagAPIServiceURL = endpoint
		return config
	}

	// Fallback to standard GRPC endpoint
	if endpoint := os.Getenv("RAG_API_SERVICE_GRPC_ENDPOINT"); endpoint != "" {
		config.RagAPIServiceURL = endpoint
		return config
	}

	// Fallback to standard URL
	if endpoint := os.Getenv("RAG_API_SERVICE_URL"); endpoint != "" {
		config.RagAPIServiceURL = endpoint
		return config
	}

	logger.Base().Warn("No RAG API service endpoint found in environment, using default", zap.String("url", config.RagAPIServiceURL))
	return config
}
