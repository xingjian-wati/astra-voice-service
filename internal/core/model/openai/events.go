package openai

import (
	"fmt"
	"strings"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

const openAISessionExpiredCode = "session_expired"

// handleModelEvent handles events from model provider and tracks conversation history
func (h *Handler) handleModelEvent(connectionID string, event map[string]interface{}) {
	// Only log critical events, filter out verbose delta events
	if eventType, ok := event["type"].(string); ok {
		if !strings.Contains(eventType, "delta") &&
			!strings.Contains(eventType, "output_audio") &&
			!strings.Contains(eventType, "audio_buffer") {
			logger.Base().Debug("OpenAI Event", zap.String("event_type", eventType), zap.String("connection_id", connectionID))
		}
	}
	eventType, ok := event["type"].(string)
	if !ok {
		return
	}
	switch eventType {
	case "error":
		h.handleErrorEvent(connectionID, event)

	case "rate_limits.updated":
		h.handleRateLimitsUpdated(connectionID, event)

	case "input_audio_buffer.speech_started":
		// User started speaking - stop silence timer AND reset retry count
		h.ResetSilenceTimer(connectionID)

	case "input_audio_buffer.speech_stopped":
		// User stopped speaking

	case "input_audio_buffer.committed":
		// Audio buffer committed - user speech ready for transcription
		logger.Base().Debug("🎙 Audio buffer committed for: - waiting for transcription...", zap.String("connection_id", connectionID))

	case "conversation.item.added":
		h.handleConversationItemAdded(connectionID, event)

	case "conversation.item.created":
		h.handleConversationItemCreated(connectionID, event)

	case "response.audio_transcript.done":
		h.handleResponseAudioTranscriptDone(connectionID, event)

	case "response.created":
		h.handleResponseCreated(connectionID)
		// Stop silence timer when AI starts responding (PAUSE only, don't reset count)
		h.PauseSilenceTimer(connectionID)

	case "response.done":
		h.handleResponseDone(connectionID, event)
		h.StartSilenceTimer(connectionID)

	// case "response.completed":
	// 	// Start silence timer when AI finishes responding
	// 	h.startSilenceTimer(connectionID)

	case "conversation.item.input_audio_transcription.completed":
		h.handleInputAudioTranscriptionCompleted(connectionID, event)

	case "response.function_call_arguments.done":
		h.handleFunctionCallArgumentsDone(connectionID, event)

	case "response.output_item.done":
		h.handleResponseOutputItemDone(connectionID, event)

	}
}

func (h *Handler) writeMessage(connectionID, role, content string) {
	if h.ConnectionGetter == nil {
		return
	}
	conn := h.ConnectionGetter(connectionID)
	if conn == nil {
		return
	}
	conn.AddMessage(role, content)
	logger.Base().Info("Added message to conversation history", zap.String("connection_id", connectionID), zap.String("role", role), zap.String("content", content))
}

func (h *Handler) handleResponseOutputItemDone(connectionID string, event map[string]interface{}) {
	logger.Base().Debug("Item created event received for", zap.String("connection_id", connectionID))
	if item, ok := event["item"].(map[string]interface{}); ok {
		if role, roleOk := item["role"].(string); roleOk {
			logger.Base().Debug("Item role", zap.String("role", role))
			if content, contentOk := item["content"].([]interface{}); contentOk && len(content) > 0 {
				if contentItem, itemOk := content[0].(map[string]interface{}); itemOk {
					if text, textOk := contentItem["text"].(string); textOk {
						// Add message to conversation history
						logger.Base().Debug("Item text", zap.String("text", text))
						// h.writeMessage(connectionID, role, text)
					}
				}
			}
		}
	}
}

// handleErrorEvent handles OpenAI error events
func (h *Handler) handleErrorEvent(connectionID string, event map[string]interface{}) {
	logger.Base().Error("OpenAI error event", zap.Any("event", event))

	var code string
	if errorData, ok := event["error"].(map[string]interface{}); ok {
		if msg, ok := errorData["message"].(string); ok {
			logger.Base().Error("", zap.String("message", msg))
		}
		if c, ok := errorData["code"].(string); ok {
			code = c
			logger.Base().Error("", zap.String("c", c))
		}
	}

	if code == openAISessionExpiredCode {
		logger.Base().Warn("Session expired for , closing connection", zap.String("connection_id", connectionID))
		h.CloseConnection(connectionID)
	}
}

// handleConversationItemAdded handles when items are added to conversation
func (h *Handler) handleConversationItemAdded(connectionID string, event map[string]interface{}) {
	if item, ok := event["item"].(map[string]interface{}); ok {
		if itemType, typeOk := item["type"].(string); typeOk && itemType == "input_audio" {
			logger.Base().Debug("🎙 Found input_audio item in item.added")
			// The transcript will be processed via conversation.item.input_audio_transcription.completed event
		}
	}
}

// handleConversationItemCreated handles when conversation items are created
func (h *Handler) handleConversationItemCreated(connectionID string, event map[string]interface{}) {
	logger.Base().Debug("Item created event received for", zap.String("connection_id", connectionID))
	if item, ok := event["item"].(map[string]interface{}); ok {
		if role, roleOk := item["role"].(string); roleOk {
			logger.Base().Debug("Item role", zap.String("role", role))
			if content, contentOk := item["content"].([]interface{}); contentOk && len(content) > 0 {
				if contentItem, itemOk := content[0].(map[string]interface{}); itemOk {
					if text, textOk := contentItem["text"].(string); textOk {
						// If this is a user message, process it for RAG
						if role == config.MessageRoleUser {
							h.processUserMessage(connectionID, text)
						}
					}
				}
			}
		}
	}
}

// handleResponseCreated handles when OpenAI starts generating response
func (h *Handler) handleResponseCreated(connectionID string) {
	logger.Base().Info("OpenAI response created for", zap.String("connection_id", connectionID))
}

// handleResponseDone handles when OpenAI completes response
func (h *Handler) handleResponseDone(connectionID string, event map[string]interface{}) {
	logger.Base().Info("OpenAI response done for", zap.String("connection_id", connectionID))

	// Check if this connection needs session reset (after temporary instructions)
	h.Mutex.RLock()
	needsReset := h.PendingReset[connectionID]
	h.Mutex.RUnlock()

	// Ensure we mark the connection as switched to realtime mode once ANY response is done
	// This acts as a fallback if handleResponseAudioTranscriptDone didn't trigger
	if h.ConnectionGetter != nil {
		if conn := h.ConnectionGetter(connectionID); conn != nil {
			// If greeting was sent, and we finished a response, we are definitely in realtime mode now
			if conn.IsGreetingSent() {
				conn.SetSwitchedToRealtime(true)
			}
		}
	}

	if needsReset {
		// Reset session instructions after this response completes
		defer func() {
			if err := h.resetSessionInstructions(connectionID); err != nil {
				logger.Base().Error("Failed to reset session instructions for", zap.String("connection_id", connectionID))
			}
		}()
	}

	// Check for function calls and usage statistics in response.done
	if response, ok := event["response"].(map[string]interface{}); ok {
		// Extract usage statistics
		if usage, ok := response["usage"].(map[string]interface{}); ok {
			totalTokens, _ := usage["total_tokens"].(float64)
			inputTokens, _ := usage["input_tokens"].(float64)
			outputTokens, _ := usage["output_tokens"].(float64)
			logger.Base().Info("📊 OpenAI Response Usage",
				zap.String("connection_id", connectionID),
				zap.Float64("total_tokens", totalTokens),
				zap.Float64("input_tokens", inputTokens),
				zap.Float64("output_tokens", outputTokens))
		}

		if output, ok := response["output"].([]interface{}); ok {
			for _, item := range output {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if itemType, ok := itemMap["type"].(string); ok {
						if itemType == "function_call" {
							name, _ := itemMap["name"].(string)
							arguments, _ := itemMap["arguments"].(string)
							callID, _ := itemMap["call_id"].(string)

							logger.Base().Info("Function call detected: (callID: , args: )", zap.String("name", name), zap.String("arguments", arguments), zap.String("call_id", callID))

							// Execute function asynchronously
							go h.executeFunctionCall(connectionID, callID, name, arguments)
						}
						if itemType == "message" {
							// Check if this is a summary response by examining the content
							// Only add to conversation history if it's NOT a summary response
							// Summary responses are out-of-band and should not be written to conversation
							h.handleAssistantMessageOutput(connectionID, itemMap)

						}
					}
				}
			}

		}
	}
}

// handleInputAudioTranscriptionCompleted handles when user's audio transcription is completed
func (h *Handler) handleInputAudioTranscriptionCompleted(connectionID string, event map[string]interface{}) {
	logger.Base().Debug("🎙 Transcription completed event received for", zap.String("connection_id", connectionID))
	if transcript, ok := event["transcript"].(string); ok && transcript != "" {
		// Add to conversation history
		h.writeMessage(connectionID, config.MessageRoleUser, transcript)

		// Log transcript and logprobs
		logFields := []zap.Field{
			zap.String("transcript", transcript),
			zap.String("connection_id", connectionID),
		}

		// Check for logprobs in the event
		if logprobs, ok := event["logprobs"]; ok {
			logFields = append(logFields, zap.Any("logprobs", logprobs))
		}

		logger.Base().Info("User transcript completed", logFields...)

		// Process user input for RAG and language guidance
		h.processUserMessage(connectionID, transcript)
	} else {
		logger.Base().Debug("No transcript found in transcription event for", zap.String("connection_id", connectionID))
		logger.Base().Debug("Event data", zap.Any("event", event))
	}
}

// handleFunctionCallArgumentsDone handles when function call arguments are completed
func (h *Handler) handleFunctionCallArgumentsDone(connectionID string, event map[string]interface{}) {
	logger.Base().Info("Arguments done for", zap.String("connection_id", connectionID))
	if item, ok := event["item"].(map[string]interface{}); ok {
		if call, ok := item["call"].(map[string]interface{}); ok {
			functionName, _ := call["name"].(string)
			arguments, _ := call["arguments"].(string)
			callID, _ := call["call_id"].(string)

			logger.Base().Info("Function call ready: (callID: )", zap.String("functionname", functionName), zap.String("call_id", callID))
			go h.executeFunctionCall(connectionID, callID, functionName, arguments)
		}
	}
}

// handleAssistantMessageOutput handles assistant message output from response.done
func (h *Handler) handleAssistantMessageOutput(connectionID string, itemMap map[string]interface{}) {
	logger.Base().Info("💬 Processing output for", zap.String("connection_id", connectionID))

	// Extract role (should be "assistant")
	role, _ := itemMap["role"].(string)

	// Extract content array
	if content, ok := itemMap["content"].([]interface{}); ok {
		for _, contentItem := range content {
			if contentMap, ok := contentItem.(map[string]interface{}); ok {
				// Check if this is an output_audio type with transcript
				contentType, _ := contentMap["type"].(string)
				if contentType == "audio" || contentType == "output_audio" {
					if transcript, ok := contentMap["transcript"].(string); ok && transcript != "" {
						// Add assistant message to conversation history
						h.writeMessage(connectionID, role, transcript)
					}
				}
			}
		}
	}
}

// handleRateLimitsUpdated handles rate_limits.updated events from OpenAI
func (h *Handler) handleRateLimitsUpdated(connectionID string, event map[string]interface{}) {
	logger.Base().Info("⚡ Updated for", zap.String("connection_id", connectionID))

	if rateLimits, ok := event["rate_limits"].([]interface{}); ok {
		for _, limit := range rateLimits {
			if limitMap, ok := limit.(map[string]interface{}); ok {
				name, _ := limitMap["name"].(string)
				remaining, _ := limitMap["remaining"].(float64)
				limitVal, _ := limitMap["limit"].(float64)
				resetSeconds, _ := limitMap["reset_seconds"].(float64)

				// Log rate limit information
				logger.Base().Info("Rate limit info", zap.String("name", name), zap.Float64("remaining", remaining), zap.Float64("limit", limitVal), zap.Float64("reset_seconds", resetSeconds))

				// Warn if approaching limit
				if limitVal > 0 {
					percentage := (remaining / limitVal) * 100
					if percentage < 10 {
						logger.Base().Warn("Rate limit approaching capacity", zap.String("name", name), zap.Float64("percentage", percentage))
					}
				}
			}
		}
	}
}

// sendEvent is the unified entry point for sending events to model provider, automatically detects and records instruction modifications
func (h *Handler) sendEvent(connectionID string, event map[string]interface{}) error {
	conn, exists := h.GetConnection(connectionID)
	if !exists || conn == nil {
		return fmt.Errorf("no model connection found for connection: %s", connectionID)
	}

	// Check if temporary instructions are included (will override session-level instructions)
	eventType, _ := event["type"].(string)
	if eventType == "response.create" {
		if response, ok := event["response"].(map[string]interface{}); ok {
			if instructions, hasInstructions := response["instructions"]; hasInstructions {
				logger.Base().Warn("Temporary instructions detected for connection , will reset after response", zap.String("connection_id", connectionID))
				logger.Base().Info("Temporary instructions content", zap.Any("instructions", instructions))

				// Mark this connection as needing reset after response
				h.Mutex.Lock()
				h.PendingReset[connectionID] = true
				h.Mutex.Unlock()
			}
		}
	}

	// Send event
	return conn.SendEvent(event)
}
