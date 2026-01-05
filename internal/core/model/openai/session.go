package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// Token request/response structures (centralized)
type EphemeralTokenRequest struct {
	Session SessionConfig `json:"session"`
}

type SessionConfig struct {
	Type         string        `json:"type"`
	Model        string        `json:"model"`
	Audio        AudioConfig   `json:"audio"`
	Instructions string        `json:"instructions,omitempty"` // System instructions for the model
	Tools        []interface{} `json:"tools,omitempty"`        // Function tools including RAG query
	Language     string        `json:"language,omitempty"`     // Language for the model
}

// VADParams represents Voice Activity Detection parameters
type VADParams struct {
	Threshold         float64 `json:"threshold"`
	PrefixPaddingMs   int     `json:"prefix_padding_ms"`
	SilenceDurationMs int     `json:"silence_duration_ms"`
}

type AudioConfig struct {
	Output AudioOutputConfig `json:"output"`
}

type AudioOutputConfig struct {
	Voice string  `json:"voice"`
	Speed float64 `json:"speed,omitempty"` // Speech speed: 0.25 to 4.0
}

type EphemeralTokenResponse struct {
	Value     string      `json:"value"`
	ExpiresAt interface{} `json:"expires_at"` // Can be string or number
}

// RealtimeTokenHandler handles ephemeral token generation for OpenAI Realtime API
type RealtimeTokenHandler struct {
	config *config.WebSocketConfig
}

// NewRealtimeTokenHandler creates a new token handler
func NewRealtimeTokenHandler(cfg *config.WebSocketConfig) *RealtimeTokenHandler {
	return &RealtimeTokenHandler{
		config: cfg,
	}
}

// GenerateEphemeralToken godoc
// @Summary Generate ephemeral token
// @Description Generate an ephemeral token for OpenAI Realtime API with optional custom configuration
// @Tags openai
// @Accept json
// @Produce json
// @Param voice query string false "Voice to use (e.g., alloy, echo, verse)"
// @Param speed query float64 false "Speech speed (0.25 to 4.0)"
// @Param instructions query string false "Custom instructions for the model"
// @Success 200 {object} object "Token generated successfully"
// @Failure 500 {object} string "Failed to generate token"
// @Router /realtime/token [get]
// @Router /realtime/token [post]
func (h *RealtimeTokenHandler) GenerateEphemeralToken(w http.ResponseWriter, r *http.Request) {
	logger.Base().Info("🔑 Received HTTP request for ephemeral token")

	// Get test configuration if available
	voice := "verse"   // Default voice
	speed := 1.1       // Default speed
	instructions := "" // Custom instructions

	// Check for test configuration in query parameters or custom endpoint
	if testVoice := r.URL.Query().Get("voice"); testVoice != "" {
		voice = testVoice
	}
	if testSpeed := r.URL.Query().Get("speed"); testSpeed != "" {
		if parsedSpeed, err := strconv.ParseFloat(testSpeed, 64); err == nil && parsedSpeed > 0 && parsedSpeed <= 4.0 {
			speed = parsedSpeed
		}
	}
	if testInstructions := r.URL.Query().Get("instructions"); testInstructions != "" {
		instructions = testInstructions
	}

	// Create session configuration for HTTP clients
	tokenReq := EphemeralTokenRequest{
		Session: SessionConfig{
			Type:         "realtime",
			Model:        "gpt-realtime", // Centralized model configuration
			Instructions: instructions,   // Custom prompt instructions
			Audio: AudioConfig{
				Output: AudioOutputConfig{
					Voice: voice, // Configurable voice
					Speed: speed, // Configurable speed
				},
			},
			Tools: []interface{}{
				map[string]interface{}{
					"type":        "function",
					"name":        "book_wati_demo",
					"description": "Send a WhatsApp template to help the user book a demo when intent is clear or BANT is complete.",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"whatsappNumber": map[string]interface{}{
								"type":        "string",
								"description": "User's WhatsApp number with country code (digits only preferred)",
							},
							"meetingTime": map[string]interface{}{
								"type":        "string",
								"description": "Meeting time in ISO 8601 format in Hong Kong timezone (UTC+8) (e.g., '2025-09-22T07:00:00Z' for 3 PM Hong Kong time). Do not accept natural language time expressions.",
							},
						},
						"required": []string{"whatsappNumber", "meetingTime"},
					},
				},
			},
		},
	}

	logger.Base().Info("Using voice configuration", zap.String("voice", voice), zap.Float64("speed", speed))

	// Generate token using internal method
	token, err := h.GenerateTokenInternal(tokenReq)
	if err != nil {
		logger.Base().Error("Error generating token")
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Return token response
	response := map[string]interface{}{
		"value":      token,
		"expires_at": time.Now().Add(24 * time.Hour).Unix(), // Example expiry
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
	logger.Base().Info("Successfully generated ephemeral token via HTTP")
}

// generateTokenInternal generates an ephemeral token with custom configuration (internal use)
func (h *RealtimeTokenHandler) GenerateTokenInternal(tokenReq EphemeralTokenRequest) (string, error) {
	// Create session configuration from request with transcription support
	sessionConfig := map[string]interface{}{
		"session": map[string]interface{}{
			"type":  tokenReq.Session.Type,
			"model": tokenReq.Session.Model,
			"audio": map[string]interface{}{
				"output": map[string]interface{}{
					"voice": tokenReq.Session.Audio.Output.Voice,
				},
				"input": map[string]interface{}{
					"format": map[string]interface{}{
						"type": "audio/pcm",
						"rate": 24000,
					},
					"noise_reduction": map[string]interface{}{
						"type": "near_field",
					},
					"transcription": map[string]interface{}{
						"model": "gpt-4o-transcribe",
						// "prompt": "Expect conversation about Wati and WhatsApp integration",
					},
					"turn_detection": map[string]interface{}{
						"type":                "server_vad",
						"threshold":           0.5,
						"prefix_padding_ms":   300,
						"silence_duration_ms": 700,
						// Note: Optimized for third-party DTX scenarios, faster response to user pauses
					},
				},
			},
			"include": []string{
				"item.input_audio_transcription.logprobs",
			},
		},
	}

	if tokenReq.Session.Language != "" {
		sessionConfig["session"].(map[string]interface{})["audio"].(map[string]interface{})["input"].(map[string]interface{})["transcription"].(map[string]interface{})["language"] = tokenReq.Session.Language
	}

	// Add tools configuration if present (tools now contain function tools including RAG query)
	if len(tokenReq.Session.Tools) > 0 {
		sessionConfig["session"].(map[string]interface{})["tools"] = tokenReq.Session.Tools
	}

	// Add speed if specified
	if tokenReq.Session.Audio.Output.Speed > 0 {
		sessionConfig["session"].(map[string]interface{})["audio"].(map[string]interface{})["output"].(map[string]interface{})["speed"] = tokenReq.Session.Audio.Output.Speed
	}

	// Optimize VAD parameters based on voice selection (language-specific)
	if voice := tokenReq.Session.Audio.Output.Voice; voice != "" {
		vadParams := h.getOptimalVADParams(voice)
		turnDetection := sessionConfig["session"].(map[string]interface{})["audio"].(map[string]interface{})["input"].(map[string]interface{})["turn_detection"].(map[string]interface{})
		turnDetection["threshold"] = vadParams.Threshold
		turnDetection["prefix_padding_ms"] = vadParams.PrefixPaddingMs
		turnDetection["silence_duration_ms"] = vadParams.SilenceDurationMs
	}

	// Add instructions if specified
	if tokenReq.Session.Instructions != "" {
		sessionConfig["session"].(map[string]interface{})["instructions"] = tokenReq.Session.Instructions
	}

	// Marshal the session configuration to JSON
	sessionConfigJSON, err := json.Marshal(sessionConfig)
	if err != nil {
		return "", fmt.Errorf("failed to marshal session config: %w", err)
	}

	// Make a request to OpenAI REST API to mint an ephemeral key
	openaiURL := "https://api.openai.com/v1/realtime/client_secrets"
	req, err := http.NewRequest("POST", openaiURL, bytes.NewBuffer(sessionConfigJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create OpenAI request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+h.config.OpenAIAPIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request to OpenAI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errorBody)
		return "", fmt.Errorf("OpenAI API returned status %d: %v", resp.StatusCode, errorBody)
	}

	var tokenResp EphemeralTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode OpenAI response: %w", err)
	}

	logger.Base().Info("Generated ephemeral token", zap.Any("expires_at", tokenResp.ExpiresAt))
	return tokenResp.Value, nil
}

// getOptimalVADParams returns optimized VAD parameters based on voice/language
func (h *RealtimeTokenHandler) getOptimalVADParams(voice string) VADParams {
	// Optimization: Significantly reduce silence_duration_ms to adapt to third-party DTX scenarios
	// DTX produces intermittent silence frames, requiring faster VAD response
	defaultParams := VADParams{
		Threshold:         0.6,
		PrefixPaddingMs:   150,
		SilenceDurationMs: 400,
	}

	// Language-specific optimizations based on voice
	switch voice {
	case "nova": // Chinese voices - need more sensitivity for tonal languages
		return VADParams{
			Threshold:         0.35, // Lower threshold for tonal languages
			PrefixPaddingMs:   200,  // More padding for tonal clarity
			SilenceDurationMs: 400,
		}
	case "amber": // Spanish - fast-paced language
		return VADParams{
			Threshold:         0.38, // Slightly lower threshold
			PrefixPaddingMs:   120,  // Less padding for fast speech
			SilenceDurationMs: 450,
		}
	case "marin": // English - conversational
		return VADParams{
			Threshold:         0.5,
			PrefixPaddingMs:   150, // Standard padding
			SilenceDurationMs: 500,
		}
	case "alloy": // Neutral voice - conservative settings
		return VADParams{
			Threshold:         0.52,
			PrefixPaddingMs:   180, // More padding for clarity
			SilenceDurationMs: 500,
		}
	case "verse": // Default OpenAI voice
		return VADParams{
			Threshold:         0.45,
			PrefixPaddingMs:   140,
			SilenceDurationMs: 450,
		}
	default:
		return defaultParams
	}
}

// HandleCORS handles OPTIONS requests for CORS preflight
func (h *RealtimeTokenHandler) HandleCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)
}
