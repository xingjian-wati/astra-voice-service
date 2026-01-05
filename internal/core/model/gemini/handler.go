package gemini

import (
	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
)

// Handler inherits from provider.BaseHandler, managing Gemini WebRTC connections and their lifecycle.
type Handler struct {
	*provider.BaseHandler
}

// NewGeminiHandler creates a new Gemini handler.
func NewGeminiHandler(cfg *config.WebSocketConfig) provider.ModelHandler {
	p := NewProvider(cfg)
	h := &Handler{
		BaseHandler: provider.NewBaseHandler(cfg, p),
	}

	// Set BaseHandler callbacks, which will be triggered by the provider layer.
	h.OnInactivityTimeout = h.sendInactivityMessage
	h.OnExitTimeout = h.sendExitMessage
	h.OnSendInitialGreeting = h.sendInitialGreeting

	return h
}

// InitializeConnectionWithLanguage initializes a model connection with specific language.
func (h *Handler) InitializeConnectionWithLanguage(connectionID, language, accent string) (provider.ModelConnection, error) {
	return h.initializeConnectionInternal(connectionID, language, accent)
}

// SendInitialGreetingWithLanguage sends language-specific initial greeting.
func (h *Handler) SendInitialGreetingWithLanguage(connectionID, language string) error {
	return h.sendInitialGreetingWithLanguage(connectionID, language)
}
