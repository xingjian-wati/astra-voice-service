package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/storage"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// handleResponseAudioTranscriptDone handles when assistant's audio transcript is done
func (h *Handler) handleResponseAudioTranscriptDone(connectionID string, event map[string]interface{}) {
	if transcript, ok := event["transcript"].(string); ok && transcript != "" {
		// Add to conversation history
		if h.ConnectionGetter != nil {
			if conn := h.ConnectionGetter(connectionID); conn != nil {
				conn.AddMessage(config.MessageRoleAssistant, transcript)
			}
		}

		logger.Base().Info("Added assistant transcript to history")

		// Check if this is the first assistant message (initial greeting)
		if h.ConnectionGetter != nil {
			if conn := h.ConnectionGetter(connectionID); conn != nil {
				conversationHistory := conn.GetConversationHistory()
				if len(conversationHistory) == 1 && conversationHistory[0].Role == config.MessageRoleAssistant {
					logger.Base().Info("finished initial greeting, VAD already active", zap.String("connection_id", connectionID))
					// Mark that greeting is finished and we can switch to realtime VAD handling
					conn.SetSwitchedToRealtime(true)
				}
			}
		}
	}
}

// reprocessAudioAsync extracts audio for a low-confidence transcription and re-transcribes using Whisper
func (h *Handler) reprocessAudioAsync(connectionID, messageID, itemID, oldTranscript string, oldConfidence float64) {
	// 1. Extract audio from cache
	audioCache := storage.GetAudioCache()
	if audioCache == nil {
		return
	}

	// Ensure we release the reference when finished
	defer audioCache.CleanupConnection(connectionID)

	// 2. Get timings from state
	h.Mutex.RLock()
	state, exists := h.ConnectionStates[connectionID]
	if !exists {
		h.Mutex.RUnlock()
		return
	}
	timing, timingExists := state.ItemTimings[itemID]
	h.Mutex.RUnlock()

	if !timingExists {
		logger.Base().Error("No timing found for item", zap.String("item_id", itemID))
		return
	}

	audioData, err := audioCache.GetAudioDataRange(connectionID, storage.AudioTypeWhatsAppInput, timing.StartTime, timing.EndTime)
	if err != nil {
		logger.Base().Error("Failed to extract audio for reprocessing", zap.String("item_id", itemID), zap.Error(err))
		return
	}

	// 3. Transcribe with Whisper
	transcript, confidence, err := h.transcribeWithWhisper(audioData)
	if err != nil {
		logger.Base().Error("Whisper re-transcription failed", zap.String("item_id", itemID), zap.Error(err))
		return
	}

	// 4. Update the message
	if h.ConnectionGetter != nil {
		conn := h.ConnectionGetter(connectionID)
		if conn != nil {
			if err := conn.UpdateMessage(messageID, transcript, confidence, oldTranscript, oldConfidence); err != nil {
				logger.Base().Error("Failed to update message", zap.String("message_id", messageID), zap.Error(err))
			}
		}
	}
}

// transcribeWithWhisper calls OpenAI Whisper API to transcribe audio data
func (h *Handler) transcribeWithWhisper(audioData []byte) (string, float64, error) {
	if len(audioData) == 0 {
		return "", 0, fmt.Errorf("empty audio data")
	}

	url := fmt.Sprintf("%s/v1/audio/transcriptions", h.Config.OpenAIBaseURL)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Create form file for audio
	// Use .ogg extension as Whisper API requires one of the supported formats
	part, err := writer.CreateFormFile("file", "audio.ogg")
	if err != nil {
		return "", 0, err
	}
	if _, err := part.Write(audioData); err != nil {
		return "", 0, err
	}

	// Add other fields
	_ = writer.WriteField("model", "gpt-4o-transcribe")
	_ = writer.WriteField("response_format", "json")

	if err := writer.Close(); err != nil {
		return "", 0, err
	}

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return "", 0, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+h.Config.OpenAIAPIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("whisper API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, err
	}

	// Whisper doesn't return confidence in simple JSON format,
	// but we've successfully re-transcribed it, so we can give it a high confidence value
	return result.Text, config.DefaultConfidenceThreshold, nil
}
