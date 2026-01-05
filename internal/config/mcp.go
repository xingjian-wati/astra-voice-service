package config

import (
	"os"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// MCPServiceConfig holds the MCP service configuration
type MCPServiceConfig struct {
	MCPServiceURL string
}

// DefaultMCPServiceConfig holds the default MCP service configuration values
var DefaultMCPServiceConfig = MCPServiceConfig{
	MCPServiceURL: "http://localhost:8007",
}

// LoadMCPServiceConfig loads MCP service configuration from environment variables
func LoadMCPServiceConfig() MCPServiceConfig {
	config := DefaultMCPServiceConfig

	// Load from ASTRA_COMPOSIO_MCP_SERVICE_INTERNAL_ENDPOINT
	if endpoint := os.Getenv("ASTRA_COMPOSIO_MCP_SERVICE_INTERNAL_ENDPOINT"); endpoint != "" {
		config.MCPServiceURL = endpoint
		return config
	}

	logger.Base().Warn("No MCP service endpoint found in environment, using default", zap.String("url", config.MCPServiceURL))
	return config
}
