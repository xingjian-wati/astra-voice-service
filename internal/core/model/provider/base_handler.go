package provider

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/event"
	"github.com/ClareAI/astra-voice-service/internal/core/tool"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// ExitReason indicates why we are ending the call.
type ExitReason string

const (
	ExitReasonTimeout ExitReason = "timeout"
	ExitReasonSilence ExitReason = "silence"
	ExitReasonDefault ExitReason = "default"
)

const DefaultMaxCallDurationSeconds = 300 // 5 minutes safety net for orphan sessions

type SpeechTiming struct {
	StartTime time.Time
	EndTime   time.Time
}

// ConnectionState tracks the realtime state of a connection
type ConnectionState struct {
	MaxCallTimer       *time.Timer
	SilenceTimer       *time.Timer
	RetryCount         int
	SilenceConfig      *config.SilenceConfig
	LastAudioActivity  int64 // unix nano
	CurrentSpeechStart time.Time
	ItemTimings        map[string]*SpeechTiming
}

// BaseHandler holds common connection lifecycle/state logic and external dependencies.
// It is intended to be embedded by concrete provider handlers (e.g., OpenAI, Gemini).
type BaseHandler struct {
	Connections         map[string]ModelConnection
	SessionInstructions map[string]string
	PendingReset        map[string]bool
	GreetingSignals     map[string]chan struct{}
	ConnectionStates    map[string]*ConnectionState
	FunctionCallCounts  map[string]int
	CurrentLanguages    map[string]string
	CurrentAccents      map[string]string
	Mutex               sync.RWMutex

	// Internal engine state
	Config   *config.WebSocketConfig
	Provider ModelProvider

	// External dependencies - shared across all model providers
	TokenGenerator    func(sessionType, model, voice, language string, speed float64, tools []interface{}) (string, error)
	RAGProcessor      func(userInput, connectionID string) (bool, string, string)
	LanguageDetector  func(text string) (string, error)
	ConnectionGetter  func(connectionID string) CallConnection
	ToolManager       *tool.ToolManager
	PromptGenerator   func(connectionID string) config.PromptGenerator
	AgentConfigGetter func(ctx context.Context, agentID string, channelType string) (*config.AgentConfig, error)
	EventBusGetter    func() event.EventBus
	OnConnectionClose func(connectionID string)

	// Provider-specific callbacks that must be set by the embedding handler
	OnInactivityTimeout   func(connectionID string, message string)
	OnExitTimeout         func(connectionID string, reason ExitReason)
	OnSendInitialGreeting func(connectionID string) error
}

// NewBaseHandler creates a base handler with common lifecycle/state maps and optional provider.
func NewBaseHandler(cfg *config.WebSocketConfig, p ModelProvider) *BaseHandler {
	return &BaseHandler{
		Connections:         make(map[string]ModelConnection),
		SessionInstructions: make(map[string]string),
		PendingReset:        make(map[string]bool),
		GreetingSignals:     make(map[string]chan struct{}),
		ConnectionStates:    make(map[string]*ConnectionState),
		FunctionCallCounts:  make(map[string]int),
		CurrentLanguages:    make(map[string]string),
		CurrentAccents:      make(map[string]string),
		Config:              cfg,
		Provider:            p,
	}
}

// CloseConnection handles the full closure of a connection, including cleanup and provider-specific Close().
func (h *BaseHandler) CloseConnection(connectionID string) {
	h.Mutex.Lock()
	conn, exists := h.Connections[connectionID]
	var signalChan chan struct{}

	// Stop timers if state exists
	if state, ok := h.ConnectionStates[connectionID]; ok {
		if state.MaxCallTimer != nil {
			state.MaxCallTimer.Stop()
		}
		if state.SilenceTimer != nil {
			state.SilenceTimer.Stop()
		}
		delete(h.ConnectionStates, connectionID)
	}

	if exists {
		delete(h.Connections, connectionID)
		delete(h.SessionInstructions, connectionID)
		delete(h.PendingReset, connectionID)
		delete(h.CurrentLanguages, connectionID)
		delete(h.CurrentAccents, connectionID)

		if sc, hasSignal := h.GreetingSignals[connectionID]; hasSignal {
			signalChan = sc
			delete(h.GreetingSignals, connectionID)
		}
	}
	h.Mutex.Unlock()

	// Perform actual closing outside the lock
	if exists && conn != nil {
		conn.Close()
		if signalChan != nil {
			close(signalChan)
		}

		if h.OnConnectionClose != nil {
			// Execute in goroutine to avoid potential deadlock
			go h.OnConnectionClose(connectionID)
		}
	}
}

// GetConnection safely gets a connection by connectionID
func (h *BaseHandler) GetConnection(connectionID string) (ModelConnection, bool) {
	h.Mutex.RLock()
	defer h.Mutex.RUnlock()
	conn, exists := h.Connections[connectionID]
	return conn, exists
}

// StoreConnection safely stores a connection
func (h *BaseHandler) StoreConnection(connectionID string, conn ModelConnection) {
	h.Mutex.Lock()
	defer h.Mutex.Unlock()
	h.Connections[connectionID] = conn
}

// MarkFunctionCallStart increments the active function call counter for a connection.
// It returns a cleanup function that should be deferred to decrement the counter.
func (h *BaseHandler) MarkFunctionCallStart(connectionID string) func() {
	h.Mutex.Lock()
	h.FunctionCallCounts[connectionID]++
	h.Mutex.Unlock()

	return func() {
		h.Mutex.Lock()
		if count, ok := h.FunctionCallCounts[connectionID]; ok {
			if count <= 1 {
				delete(h.FunctionCallCounts, connectionID)
			} else {
				h.FunctionCallCounts[connectionID] = count - 1
			}
		}
		h.Mutex.Unlock()
	}
}

// IsFunctionCallActive returns true if there is an active function call for the connection.
func (h *BaseHandler) IsFunctionCallActive(connectionID string) bool {
	h.Mutex.RLock()
	defer h.Mutex.RUnlock()
	return h.FunctionCallCounts[connectionID] > 0
}

// EnableGreetingSignalControl enables signal-based greeting control for a connection.
func (h *BaseHandler) EnableGreetingSignalControl(connectionID string) {
	h.Mutex.Lock()
	defer h.Mutex.Unlock()
	if _, exists := h.GreetingSignals[connectionID]; !exists {
		h.GreetingSignals[connectionID] = make(chan struct{}, 1)
	}
}

// TriggerGreeting sends a signal to trigger the greeting for a connection.
func (h *BaseHandler) TriggerGreeting(connectionID string) {
	h.Mutex.Lock()
	defer h.Mutex.Unlock()
	if signalChan, exists := h.GreetingSignals[connectionID]; exists {
		select {
		case signalChan <- struct{}{}:
			logger.Base().Info("Greeting triggered", zap.String("connection_id", connectionID))
		default:
		}
	}
}

// IsGreetingSignalControlEnabled checks if signal control is enabled for a connection.
func (h *BaseHandler) IsGreetingSignalControlEnabled(connectionID string) bool {
	h.Mutex.RLock()
	defer h.Mutex.RUnlock()
	_, exists := h.GreetingSignals[connectionID]
	return exists
}

// GetGreetingSignalChan safely gets the greeting signal channel for a connection.
func (h *BaseHandler) GetGreetingSignalChan(connectionID string) (chan struct{}, bool) {
	h.Mutex.RLock()
	defer h.Mutex.RUnlock()
	signalChan, exists := h.GreetingSignals[connectionID]
	return signalChan, exists
}

// InitConnectionState initializes the connection state and starts max duration timer.
func (h *BaseHandler) InitConnectionState(connectionID string, maxDuration int, silenceConfig *config.SilenceConfig) {
	h.Mutex.Lock()
	defer h.Mutex.Unlock()

	state := &ConnectionState{
		SilenceConfig:     silenceConfig,
		LastAudioActivity: time.Now().UnixNano(),
		ItemTimings:       make(map[string]*SpeechTiming),
	}

	if maxDuration <= 0 {
		maxDuration = DefaultMaxCallDurationSeconds
	}
	state.MaxCallTimer = time.AfterFunc(time.Duration(maxDuration)*time.Second, func() {
		logger.Base().Info("Max call duration reached", zap.String("connection_id", connectionID))
		if h.OnExitTimeout != nil {
			h.OnExitTimeout(connectionID, ExitReasonTimeout)
		}
	})
	logger.Base().Info("Max call duration timer started", zap.String("connection_id", connectionID), zap.Int("max_duration_seconds", maxDuration))

	h.ConnectionStates[connectionID] = state
}

// StartSilenceTimer starts or restarts the silence timer using the default timeout handler.
func (h *BaseHandler) StartSilenceTimer(connectionID string) {
	h.Mutex.Lock()
	defer h.Mutex.Unlock()

	state, exists := h.ConnectionStates[connectionID]
	if !exists || state.SilenceConfig == nil || state.SilenceConfig.InactivityCheckDuration <= 0 {
		return
	}

	if state.SilenceTimer != nil {
		state.SilenceTimer.Stop()
	}

	duration := time.Duration(state.SilenceConfig.InactivityCheckDuration) * time.Second
	state.SilenceTimer = time.AfterFunc(duration, func() {
		h.HandleSilenceTimeout(connectionID)
	})
	atomic.StoreInt64(&state.LastAudioActivity, time.Now().UnixNano())
}

// ResetSilenceTimer stops the silence timer and resets retry count (User spoke).
func (h *BaseHandler) ResetSilenceTimer(connectionID string) {
	h.Mutex.Lock()
	defer h.Mutex.Unlock()

	if state, exists := h.ConnectionStates[connectionID]; exists {
		if state.SilenceTimer != nil {
			state.SilenceTimer.Stop()
			state.SilenceTimer = nil
		}
		state.RetryCount = 0
	}
}

// PauseSilenceTimer stops the silence timer but keeps retry count (AI is speaking).
func (h *BaseHandler) PauseSilenceTimer(connectionID string) {
	h.Mutex.Lock()
	defer h.Mutex.Unlock()

	if state, exists := h.ConnectionStates[connectionID]; exists {
		if state.SilenceTimer != nil {
			state.SilenceTimer.Stop()
			state.SilenceTimer = nil
		}
	}
}

// HandleSilenceTimeout logic moved from OpenAI handler.
func (h *BaseHandler) HandleSilenceTimeout(connectionID string) {
	functionCallActive := h.IsFunctionCallActive(connectionID)

	h.Mutex.Lock()
	state, exists := h.ConnectionStates[connectionID]
	if !exists || state.SilenceConfig == nil {
		h.Mutex.Unlock()
		return
	}

	// Recent audio activity?
	if last := atomic.LoadInt64(&state.LastAudioActivity); last > 0 {
		if time.Since(time.Unix(0, last)) < time.Duration(state.SilenceConfig.InactivityCheckDuration)*time.Second {
			h.Mutex.Unlock()
			h.StartSilenceTimer(connectionID)
			logger.Base().Info("Recent audio activity, re-arming silence timer", zap.String("connection_id", connectionID))
			return
		}
	}

	if functionCallActive {
		h.Mutex.Unlock()
		logger.Base().Warn("Function call active, skip silence check", zap.String("connection_id", connectionID))
		h.StartSilenceTimer(connectionID)
		return
	}

	maxRetries := state.SilenceConfig.MaxRetries
	retryCount := state.RetryCount
	message := state.SilenceConfig.InactivityMessage

	state.RetryCount++
	h.Mutex.Unlock()

	if retryCount < maxRetries {
		logger.Base().Warn("Silence detected, sending prompt", zap.String("connection_id", connectionID), zap.Int("max_retries", maxRetries), zap.Int("retry_count", retryCount))
		if h.OnInactivityTimeout != nil {
			h.OnInactivityTimeout(connectionID, message)
		}
	} else {
		logger.Base().Info("Max silence retries reached, exiting", zap.String("connection_id", connectionID))
		if h.OnExitTimeout != nil {
			h.OnExitTimeout(connectionID, ExitReasonSilence)
		}
	}
}

// SetCurrentLanguageAccent records the current language and accent.
func (h *BaseHandler) SetCurrentLanguageAccent(connectionID, language, accent string) {
	h.Mutex.Lock()
	defer h.Mutex.Unlock()
	if language != "" {
		h.CurrentLanguages[connectionID] = language
	}
	if accent != "" {
		h.CurrentAccents[connectionID] = accent
	}
}

// GetCurrentLanguageAccent retrieves the current language and accent.
func (h *BaseHandler) GetCurrentLanguageAccent(connectionID string) (string, string) {
	h.Mutex.RLock()
	defer h.Mutex.RUnlock()
	return h.CurrentLanguages[connectionID], h.CurrentAccents[connectionID]
}

// MarkAudioActivity records the latest audio activity time.
func (h *BaseHandler) MarkAudioActivity(connectionID string) {
	h.Mutex.RLock()
	state, exists := h.ConnectionStates[connectionID]
	h.Mutex.RUnlock()
	if !exists {
		return
	}
	atomic.StoreInt64(&state.LastAudioActivity, time.Now().UnixNano())
}

// BuildLanguageAccentInstructions builds a markdown-style instruction string.
func (h *BaseHandler) BuildLanguageAccentInstructions(connectionID, message string) string {
	lang, accent := h.GetCurrentLanguageAccent(connectionID)

	langDisplay := "current conversation language"
	if lang != "" {
		langDisplay = fmt.Sprintf("%s (%s)", config.GetLanguageName(lang), lang)
	}

	accentDisplay := "current accent"
	accentDetail := ""
	if lang != "" && accent != "" {
		accentDisplay = fmt.Sprintf("%s accent (%s)", config.GetLanguageName(lang), accent)
		accentDetail = config.GetAccentDetailedInstruction(lang, accent)
	}

	instructions := fmt.Sprintf("**Language/Accent**: Respond in **%s** and keep **%s**.\n", langDisplay, accentDisplay)
	if accentDetail != "" {
		instructions += fmt.Sprintf("**Accent guidance**: %s\n", accentDetail)
	}
	instructions += fmt.Sprintf("**Output**: Say EXACTLY this sentence and nothing else: \"%s\".\nDo not add, change, prepend, or append any text. Do not add context.", message)
	return instructions
}

// SetOnConnectionClose sets the callback when connection is closed by logic
func (h *BaseHandler) SetOnConnectionClose(callback func(connectionID string)) {
	h.OnConnectionClose = callback
}
