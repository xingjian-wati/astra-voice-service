package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
)

// WatiClient handles communication with Wati API
type WatiClient struct {
	BaseURL         string
	TenantID        string
	APIKey          string
	HTTPClient      *http.Client
	OutboundBaseURL string // Separate base URL for outbound call APIs
}

// WatiCallAcceptRequest represents the request to accept a call
type WatiCallAcceptRequest struct {
	SDP string `json:"sdp"`
}

// WatiManualCallRequest represents manual call control request with tenant ID
type WatiManualCallRequest struct {
	TenantID string `json:"tenantId"`
	SDP      string `json:"sdp,omitempty"` // Only for accept requests
}

// WatiNewCallRequest represents the request for a new call from Wati
type WatiNewCallRequest struct {
	TenantID string `json:"tenantId"`
	CallID   string `json:"callId"`
	SDP      string `json:"sdp"`
}

// WatiTerminateCallRequest represents the request to terminate a call from Wati
type WatiTerminateCallRequest struct {
	TenantID string `json:"tenantId"`
	CallID   string `json:"callId"`
}

// WatiAPIResponse represents the response to Wati API calls
type WatiAPIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// WatiCallResponse represents the response from Wati API
type WatiCallResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// WatiWebhookEvent represents webhook events from Wati
type WatiWebhookEvent struct {
	APIKey         string             `json:"apiKey"`                   // API Key from webhook
	AgentMapping   *VoiceAgentMapping `json:"agentMapping"`             // Agent mapping from webhook
	TenantID       string             `json:"tenantId"`                 // Tenant ID from webhook
	CallID         string             `json:"callId"`                   // Call ID
	SDP            json.RawMessage    `json:"sdp,omitempty"`            // SDP data
	VoiceLanguage  string             `json:"voiceLanguage,omitempty"`  // Language from Wati (e.g., "en", "zh", "es")
	Contact        *WatiContact       `json:"contact,omitempty"`        // Contact information from Wati
	BusinessNumber string             `json:"businessNumber,omitempty"` // Business number for agent selection
}

type VoiceAgentMapping struct {
	TenantID string `json:"tenantId"`
	AgentID  string `json:"agentId"`
}

// WatiContact represents contact information from Wati webhook
type WatiContact struct {
	ContactName   string `json:"ContactName"`
	ContactNumber string `json:"ContactNumber"`
}

// WatiSDPData represents SDP data from Wati webhook
type WatiSDPData struct {
	Type string `json:"type"` // "offer"
	SDP  string `json:"sdp"`
}

// ParseSDP parses SDP data from webhook, handling both string and object formats
func (e *WatiWebhookEvent) ParseSDP() (*WatiSDPData, error) {
	if len(e.SDP) == 0 {
		return nil, nil
	}

	// Try to parse as string first
	var sdpString string
	if err := json.Unmarshal(e.SDP, &sdpString); err == nil {
		return &WatiSDPData{
			Type: "offer",
			SDP:  sdpString,
		}, nil
	}

	// Try to parse as object
	var sdpData WatiSDPData
	if err := json.Unmarshal(e.SDP, &sdpData); err == nil {
		return &sdpData, nil
	}

	return nil, fmt.Errorf("unable to parse SDP data")
}

// NewWatiClient creates a new Wati API client
func NewWatiClient(baseURL, tenantID, apiKey, outboundBaseURL string) *WatiClient {
	client := &WatiClient{
		BaseURL:         baseURL,
		TenantID:        tenantID,
		APIKey:          apiKey,
		OutboundBaseURL: outboundBaseURL,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}

	if outboundBaseURL != "" {
		logger.Base().Info("Outbound base URL configured", zap.String("outboundbaseurl", outboundBaseURL))
	} else {
		logger.Base().Info("Outbound base URL not configured, using default WATI_BASE_URL")
	}

	return client
}

// GetOutboundBaseURL returns the outbound base URL, falling back to BaseURL if not set
func (c *WatiClient) GetOutboundBaseURL() string {
	if c.OutboundBaseURL != "" {
		return c.OutboundBaseURL
	}
	return c.BaseURL
}

// AcceptCall accepts a WhatsApp call through Wati API
func (c *WatiClient) AcceptCall(callID string, sdpAnswer string) error {
	return c.AcceptCallWithTenant(c.TenantID, callID, sdpAnswer)
}

// AcceptCallWithTenant accepts a WhatsApp call with specific tenant ID
func (c *WatiClient) AcceptCallWithTenant(tenantID, callID string, sdpAnswer string) error {
	// Try different API endpoint formats
	// Original format
	url := fmt.Sprintf("%s/%s/api/v1/openapi/whatsapp/calls/%s/accept",
		c.BaseURL, tenantID, callID)
	logger.Base().Debug("Calling Wati API to accept call", zap.String("url", url))

	// Alternative format - try without tenant ID in path
	// url := fmt.Sprintf("%s/api/v1/openapi/whatsapp/calls/%s/accept",
	//	c.BaseURL, callID)
	request := WatiCallAcceptRequest{
		SDP: sdpAnswer,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	logger.Base().Info("Accepting call via Wati API")
	logger.Base().Info("SDP Answer length", zap.Int("bytes", len(sdpAnswer)))

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Try different authentication methods
	// req.Header.Set("Authorization", "Bearer "+c.APIKey)
	// req.Header.Set("X-API-Key", c.APIKey)
	// req.Header.Set("apiKey", c.APIKey)
	// req.Header.Set("X-Auth-Token", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	logger.Base().Info("Wati API response status", zap.Int("status_code", resp.StatusCode))

	// Read response body for debugging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	logger.Base().Info("Wati API response body", zap.String("body", string(bodyBytes)))

	if len(bodyBytes) == 0 {
		return fmt.Errorf("empty response from Wati API")
	}

	var response WatiCallResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	if response.Code != 200 {
		return fmt.Errorf("Wati API error: code=%d, message=%s", response.Code, response.Message)
	}

	logger.Base().Info("Call accepted successfully via Wati", zap.String("call_id", callID))
	return nil
}

// TerminateCall terminates a WhatsApp call through Wati API
func (c *WatiClient) TerminateCall(callID string) error {
	return c.TerminateCallWithTenant(c.TenantID, callID)
}

// TerminateCallWithTenant terminates a WhatsApp call with specific tenant ID
func (c *WatiClient) TerminateCallWithTenant(tenantID, callID string) error {
	url := fmt.Sprintf("%s/%s/api/v1/openapi/whatsapp/calls/%s/terminate",
		c.BaseURL, tenantID, callID)

	logger.Base().Info("Terminating call via Wati API", zap.String("url", url), zap.String("call_id", callID))

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	// req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	var response WatiCallResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	if response.Code != 200 {
		return fmt.Errorf("Wati API error: code=%d, message=%s", response.Code, response.Message)
	}

	logger.Base().Info("Call terminated successfully via Wati", zap.String("call_id", callID))
	return nil
}

// GetCallPermissions gets call permissions for a WhatsApp user from external Wati API
// tenantID is required - it will be used in the URL path for multi-tenant scenarios
func (c *WatiClient) GetCallPermissions(waid string, channelPhoneNumber string, tenantID string) (map[string]interface{}, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID is required for outbound call API")
	}

	baseURL := c.GetOutboundBaseURL()

	// Build URL with tenant ID in path: baseURL/tenantID/api/v1/...
	url := fmt.Sprintf("%s/%s/api/v1/openapi/whatsapp/calls/permissions/%s", baseURL, tenantID, waid)

	if channelPhoneNumber != "" {
		url = fmt.Sprintf("%s?channelPhoneNumber=%s", url, channelPhoneNumber)
	}

	logger.Base().Info("Getting call permissions for WAID (tenant: ) via Wati API", zap.String("tenant_id", tenantID), zap.String("waid", waid), zap.String("url", url))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	logger.Base().Info("Wati API response status", zap.Int("status_code", resp.StatusCode), zap.String("body", string(bodyBytes)))

	var response map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	logger.Base().Info("Successfully retrieved call permissions for WAID", zap.String("waid", waid))
	return response, nil
}

// MakeOutboundCallRequest represents the request to make an outbound call
type MakeOutboundCallRequest struct {
	Sdp string `json:"Sdp"`
}

// MakeOutboundCallResponse represents the response from making an outbound call
type MakeOutboundCallResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Result  struct {
		CallID string `json:"callId"`
	} `json:"result,omitempty"`
}

// MakeOutboundCall makes an outbound call to a WhatsApp user via external Wati API
// tenantID is required - it will be used in the URL path for multi-tenant scenarios
func (c *WatiClient) MakeOutboundCall(waid string, sdp string, channelPhoneNumber string, xPartner string, tenantID string) (*MakeOutboundCallResponse, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID is required for outbound call API")
	}

	baseURL := c.GetOutboundBaseURL()

	// Build URL with tenant ID in path: baseURL/tenantID/api/v1/...
	url := fmt.Sprintf("%s/%s/api/v1/openapi/whatsapp/calls/outbound-call/%s", baseURL, tenantID, waid)

	if channelPhoneNumber != "" {
		url = fmt.Sprintf("%s?channelPhoneNumber=%s", url, channelPhoneNumber)
	}

	logger.Base().Info("Making outbound call to WAID (tenant: ) via Wati API", zap.String("tenant_id", tenantID), zap.String("waid", waid), zap.String("url", url))

	request := MakeOutboundCallRequest{
		Sdp: sdp,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if xPartner != "" {
		req.Header.Set("X-Partner", xPartner)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	logger.Base().Info("Wati API response status", zap.Int("status_code", resp.StatusCode), zap.String("body", string(bodyBytes)))

	var response MakeOutboundCallResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	if response.Code != 200 {
		return nil, fmt.Errorf("Wati API error: code=%d, message=%s", response.Code, response.Message)
	}

	logger.Base().Info("Successfully initiated outbound call", zap.String("waid", waid), zap.String("call_id", response.Result.CallID))
	return &response, nil
}

// SendCallPermissionRequest sends a call permission request to a WhatsApp user via external Wati API
// tenantID is required - it will be used in the URL path for multi-tenant scenarios
func (c *WatiClient) SendCallPermissionRequest(waid string, channelPhoneNumber string, tenantID string) error {
	if tenantID == "" {
		return fmt.Errorf("tenant ID is required for outbound call API")
	}

	baseURL := c.GetOutboundBaseURL()

	// Build URL with tenant ID in path: baseURL/tenantID/api/v1/...
	url := fmt.Sprintf("%s/%s/api/v1/openapi/whatsapp/calls/call-permission-request/%s", baseURL, tenantID, waid)

	if channelPhoneNumber != "" {
		url = fmt.Sprintf("%s?channelPhoneNumber=%s", url, channelPhoneNumber)
	}

	logger.Base().Info("Sending call permission request to WAID (tenant: ) via Wati API", zap.String("tenant_id", tenantID), zap.String("waid", waid), zap.String("url", url))

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	logger.Base().Info("Wati API response status", zap.Int("status_code", resp.StatusCode), zap.String("body", string(bodyBytes)))

	var response WatiCallResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	if response.Code != 200 {
		return fmt.Errorf("Wati API error: code=%d, message=%s", response.Code, response.Message)
	}

	logger.Base().Info("Successfully sent call permission request to WAID", zap.String("waid", waid))
	return nil
}

// GenerateSDPAnswer generates an SDP answer for the given offer
func (c *WatiClient) GenerateSDPAnswer(offerSDP string) (string, error) {
	logger.Base().Info("Generating SDP answer for offer", zap.Int("offer_length", len(offerSDP)))

	// Generate a basic SDP answer based on the example you provided
	// In production, you would use a proper WebRTC library to generate this
	sessionID := time.Now().Unix()

	// Generate SDP answer following the official WhatsApp sample structure
	// Generate unique identifiers
	msid := fmt.Sprintf("%x-%x-%x-%x-%x",
		rand.Int31(), rand.Int31(), rand.Int31(), rand.Int31(), rand.Int31())
	ssrc := rand.Uint32()
	cname := fmt.Sprintf("%x", rand.Int63())

	// Generate random ice credentials
	iceUfrag := fmt.Sprintf("%x", rand.Int63())
	icePwd := fmt.Sprintf("%x", rand.Int63())

	// Generate random fingerprint (simplified for demo)
	fingerprint := "2E:35:9F:21:9E:63:72:E5:42:74:76:2D:B3:70:F7:CB:24:14:9B:14:52:71:05:48:DA:4D:67:31:09:58:2A:ED"

	sdpAnswer := fmt.Sprintf(`v=0
o=- %d 2 IN IP4 127.0.0.1
s=-
t=0 0
a=group:BUNDLE audio
a=msid-semantic: WMS %s
m=audio 9 UDP/TLS/RTP/SAVPF 111 126
c=IN IP4 0.0.0.0
a=rtcp:9 IN IP4 0.0.0.0
a=ice-ufrag:%s
a=ice-pwd:%s
a=fingerprint:sha-256 %s
a=setup:active
a=mid:audio
a=extmap:1 urn:ietf:params:rtp-hdrext:ssrc-audio-level
a=extmap:2 http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time
a=extmap:3 http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01
a=sendrecv
a=rtcp-mux
a=rtpmap:111 opus/48000/2
a=rtcp-fb:111 transport-cc
a=fmtp:111 minptime=10;useinbandfec=1
a=rtpmap:126 telephone-event/8000
a=ssrc:%d cname:%s
a=ssrc:%d msid:%s ea478c16-d9f7-493c-8cec-19bfac750a36

`, sessionID, msid, iceUfrag, icePwd, fingerprint, ssrc, cname, ssrc, msid)

	logger.Base().Info("Generated SDP answer", zap.Int("length", len(sdpAnswer)))
	return sdpAnswer, nil
}

// GenerateAgentJWT generates a JWT token for an agent with tenant ID and agent ID
func GenerateAgentJWT(tenantID, agentID string) (string, error) {
	// Get the secret key from environment
	secretKey := os.Getenv("SECRET_KEY")
	if secretKey == "" {
		return "", fmt.Errorf("SECRET_KEY not configured")
	}

	// Create token with claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"tenantId": tenantID,
		"agentId":  agentID,
		"iss":      "whatsapp-voice-service",
		"iat":      time.Now().Unix(),
	})

	// Sign the token with the secret key
	tokenString, err := token.SignedString([]byte(secretKey))
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT token: %w", err)
	}

	return tokenString, nil
}

// DecodeJWTFromAPIKey decodes the JWT token from APIKey and extracts agent mapping information
func DecodeJWTFromAPIKey(apiKey string) (*VoiceAgentMapping, error) {
	// Get the secret key from environment
	secretKey := os.Getenv("SECRET_KEY")
	if secretKey == "" {
		return nil, fmt.Errorf("SECRET_KEY not configured")
	}

	// Parse the JWT token
	token, err := jwt.Parse(apiKey, func(token *jwt.Token) (interface{}, error) {
		// Validate the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secretKey), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse JWT: %w", err)
	}

	// Extract claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Extract tenantId and agentId from claims
	tenantID, ok := claims["tenantId"].(string)
	if !ok {
		// Try alternative claim name
		tenantID, ok = claims["tenant_id"].(string)
		if !ok {
			return nil, fmt.Errorf("tenantId not found in JWT claims")
		}
	}

	agentID, ok := claims["agentId"].(string)
	if !ok {
		// Try alternative claim name
		agentID, ok = claims["agent_id"].(string)
		if !ok {
			return nil, fmt.Errorf("agentId not found in JWT claims")
		}
	}

	return &VoiceAgentMapping{
		TenantID: tenantID,
		AgentID:  agentID,
	}, nil
}
