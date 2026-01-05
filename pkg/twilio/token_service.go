package twilio

import (
	"fmt"
	"sync"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/twilio/twilio-go"
	api "github.com/twilio/twilio-go/rest/api/v2010"
	"go.uber.org/zap"
)

// TwilioTokenService manages Twilio Network Traversal Service tokens
// It fetches and caches TURN/STUN credentials from Twilio's API
type TwilioTokenService struct {
	client        *twilio.RestClient
	currentToken  *api.ApiV2010Token
	mutex         sync.RWMutex
	lastFetchTime time.Time
	enabled       bool
	accountSID    string
	authToken     string
	refreshTicker *time.Ticker
	stopChan      chan struct{}
}

// TURNCredentials represents TURN server credentials
type TURNCredentials struct {
	URLs       []string
	Username   string
	Credential string
}

// NewTwilioTokenService creates a new Twilio token service
// If accountSID or authToken is empty, the service will be disabled
func NewTwilioTokenService(accountSID, authToken string, enableAutoRefresh bool) *TwilioTokenService {
	if accountSID == "" || authToken == "" {
		logger.Base().Warn("Twilio credentials not provided, TURN service disabled")
		return &TwilioTokenService{enabled: false}
	}

	service := &TwilioTokenService{
		client:     twilio.NewRestClientWithParams(twilio.ClientParams{Username: accountSID, Password: authToken}),
		enabled:    true,
		accountSID: accountSID,
		authToken:  authToken,
		stopChan:   make(chan struct{}),
	}

	// Fetch initial token
	if err := service.RefreshToken(); err != nil {
		logger.Base().Error("Failed to fetch initial Twilio token")
		// Do not disable service on initial failure, let auto-refresh handle it
	}

	// Start auto-refresh if enabled
	if enableAutoRefresh {
		service.StartAutoRefresh()
	}

	return service
}

// RefreshToken fetches a new token from Twilio API
func (s *TwilioTokenService) RefreshToken() error {
	if !s.enabled {
		return fmt.Errorf("twilio token service is disabled")
	}

	logger.Base().Info("Fetching new Twilio TURN token...")

	params := &api.CreateTokenParams{}
	resp, err := s.client.Api.CreateToken(params)
	if err != nil {
		logger.Base().Error("Failed to fetch Twilio token")
		return err
	}

	s.mutex.Lock()
	s.currentToken = resp
	s.lastFetchTime = time.Now()
	s.mutex.Unlock()

	logger.Base().Info("Twilio TURN token refreshed successfully")
	if resp.IceServers != nil && len(*resp.IceServers) > 0 {
		logger.Base().Info("Received ICE servers from Twilio", zap.Int("count", len(*resp.IceServers)))
	}

	return nil
}

// GetTURNCredentials returns current TURN credentials
// Returns nil if service is disabled or no token is available
func (s *TwilioTokenService) GetTURNCredentials() []TURNCredentials {
	if !s.enabled {
		return nil
	}

	s.mutex.RLock()
	hasToken := s.currentToken != nil && s.currentToken.IceServers != nil
	s.mutex.RUnlock()

	// If no token available, try to fetch one immediately
	// This ensures that if Twilio was down during startup or previous refreshes,
	// we attempt to recover immediately when a call is actually made,
	// avoiding the need to restart the service.
	if !hasToken {
		logger.Base().Warn("No Twilio token available, attempting to fetch immediately...")
		if err := s.RefreshToken(); err != nil {
			logger.Base().Error("Failed to fetch Twilio token on-demand")
			return nil
		}
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if s.currentToken == nil || s.currentToken.IceServers == nil {
		return nil
	}

	credentials := make([]TURNCredentials, 0)

	for _, server := range *s.currentToken.IceServers {
		// Only include TURN servers (skip STUN)
		if server.Url != "" && len(server.Url) >= 4 && server.Url[0:4] == "turn" {
			cred := TURNCredentials{
				URLs: []string{server.Url},
			}

			if server.Username != "" {
				cred.Username = server.Username
			}

			if server.Credential != "" {
				cred.Credential = server.Credential
			}

			credentials = append(credentials, cred)
		}
	}

	return credentials
}

// GetAllICEServers returns all ICE servers (STUN + TURN) from Twilio
func (s *TwilioTokenService) GetAllICEServers() []string {
	if !s.enabled {
		return nil
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if s.currentToken == nil || s.currentToken.IceServers == nil {
		return nil
	}

	servers := make([]string, 0)
	for _, server := range *s.currentToken.IceServers {
		if server.Url != "" {
			servers = append(servers, server.Url)
		}
	}

	return servers
}

// StartAutoRefresh starts automatic token refresh every 23 hours
// Twilio tokens are valid for 24 hours, refresh 1 hour before expiration
func (s *TwilioTokenService) StartAutoRefresh() {
	if !s.enabled {
		return
	}

	refreshInterval := 23 * time.Hour
	s.refreshTicker = time.NewTicker(refreshInterval)

	go func() {
		logger.Base().Info("Started Twilio token auto-refresh", zap.Duration("refresh_interval", refreshInterval))
		for {
			select {
			case <-s.refreshTicker.C:
				if err := s.RefreshToken(); err != nil {
					logger.Base().Error("Auto-refresh failed")
				}
			case <-s.stopChan:
				logger.Base().Info("🛑 Stopped Twilio token auto-refresh")
				return
			}
		}
	}()
}

// Stop stops the auto-refresh goroutine
func (s *TwilioTokenService) Stop() {
	if s.refreshTicker != nil {
		s.refreshTicker.Stop()
		close(s.stopChan)
	}
}

// IsEnabled returns whether the service is enabled
func (s *TwilioTokenService) IsEnabled() bool {
	return s.enabled
}

// GetTokenAge returns how long ago the current token was fetched
func (s *TwilioTokenService) GetTokenAge() time.Duration {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if s.lastFetchTime.IsZero() {
		return 0
	}

	return time.Since(s.lastFetchTime)
}
