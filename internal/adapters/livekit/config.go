package livekit

import (
	"errors"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// LiveKitConfig holds LiveKit server configuration
type LiveKitConfig struct {
	ServerURL         string // LiveKit server WebSocket URL
	APIKey            string // LiveKit API key
	APISecret         string // LiveKit API secret
	DefaultRoomPrefix string // Default room name prefix
	Enabled           bool   // Whether LiveKit integration is enabled
	GCSBucket         string // GCS bucket name for egress recordings (GKE auto-configured)
}

// NewLiveKitConfig creates a new LiveKit configuration with validation
func NewLiveKitConfig(serverURL, apiKey, apiSecret, gcsBucket string) (*LiveKitConfig, error) {
	if serverURL == "" {
		return nil, errors.New("LiveKit server URL is required")
	}
	if apiKey == "" {
		return nil, errors.New("LiveKit API key is required")
	}
	if apiSecret == "" {
		return nil, errors.New("LiveKit API secret is required")
	}

	config := &LiveKitConfig{
		ServerURL:         serverURL,
		APIKey:            apiKey,
		APISecret:         apiSecret,
		DefaultRoomPrefix: "astra-",
		Enabled:           true,
		GCSBucket:         gcsBucket,
	}

	logger.Base().Info("LiveKit configuration initialized", zap.String("serverurl", serverURL))
	if gcsBucket != "" {
		logger.Base().Info("LiveKit egress GCS bucket", zap.String("gcsbucket", gcsBucket))
	}
	return config, nil
}

// Validate validates the LiveKit configuration
func (c *LiveKitConfig) Validate() error {
	if c.ServerURL == "" {
		return errors.New("LiveKit server URL is required")
	}
	if c.APIKey == "" {
		return errors.New("LiveKit API key is required")
	}
	if c.APISecret == "" {
		return errors.New("LiveKit API secret is required")
	}
	return nil
}

// IsEnabled returns whether LiveKit is enabled
func (c *LiveKitConfig) IsEnabled() bool {
	return c.Enabled && c.ServerURL != "" && c.APIKey != "" && c.APISecret != ""
}
