package model

import (
	"fmt"
	"sync"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model/gemini"
	"github.com/ClareAI/astra-voice-service/internal/core/model/openai"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
)

// DefaultProviderFactory implements ProviderFactory
type DefaultProviderFactory struct {
	providers        map[provider.ProviderType]func(*config.WebSocketConfig) provider.ModelProvider
	handlerFactories map[provider.ProviderType]func(*config.WebSocketConfig) provider.ModelHandler
	handlers         map[provider.ProviderType]provider.ModelHandler
	mutex            sync.RWMutex
}

// NewProviderFactory creates a new provider factory with default providers registered
func NewProviderFactory() *DefaultProviderFactory {
	factory := &DefaultProviderFactory{
		providers:        make(map[provider.ProviderType]func(*config.WebSocketConfig) provider.ModelProvider),
		handlerFactories: make(map[provider.ProviderType]func(*config.WebSocketConfig) provider.ModelHandler),
		handlers:         make(map[provider.ProviderType]provider.ModelHandler),
	}

	// Register default providers
	factory.RegisterProvider(provider.ProviderTypeOpenAI, func(cfg *config.WebSocketConfig) provider.ModelProvider {
		return openai.NewProvider(cfg)
	})
	factory.RegisterHandlerFactory(provider.ProviderTypeOpenAI, func(cfg *config.WebSocketConfig) provider.ModelHandler {
		return openai.NewOpenAIHandler(cfg)
	})

	// Register Gemini provider
	factory.RegisterProvider(provider.ProviderTypeGemini, func(cfg *config.WebSocketConfig) provider.ModelProvider {
		return gemini.NewProvider(cfg)
	})
	factory.RegisterHandlerFactory(provider.ProviderTypeGemini, func(cfg *config.WebSocketConfig) provider.ModelHandler {
		return gemini.NewGeminiHandler(cfg)
	})

	return factory
}

// RegisterProvider registers a provider type
func (f *DefaultProviderFactory) RegisterProvider(providerType provider.ProviderType, factory func(*config.WebSocketConfig) provider.ModelProvider) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.providers[providerType] = factory
}

// RegisterHandlerFactory registers a handler factory for a provider type
func (f *DefaultProviderFactory) RegisterHandlerFactory(providerType provider.ProviderType, handlerFactory func(*config.WebSocketConfig) provider.ModelHandler) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.handlerFactories[providerType] = handlerFactory
}

// RegisterHandler registers a pre-created handler instance for a provider type
func (f *DefaultProviderFactory) RegisterHandler(providerType provider.ProviderType, handler provider.ModelHandler) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.handlers[providerType] = handler
}

// CreateProvider creates a provider instance
func (f *DefaultProviderFactory) CreateProvider(providerType provider.ProviderType, config *config.WebSocketConfig) (provider.ModelProvider, error) {
	f.mutex.RLock()
	factory, exists := f.providers[providerType]
	f.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
	return factory(config), nil
}

// CreateHandler creates or returns a cached handler instance
func (f *DefaultProviderFactory) CreateHandler(providerType provider.ProviderType, config *config.WebSocketConfig) (provider.ModelHandler, error) {
	// First check if already created
	f.mutex.RLock()
	handler, exists := f.handlers[providerType]
	f.mutex.RUnlock()

	if exists {
		return handler, nil
	}

	// Not created yet, need to create and cache
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// Double check under write lock
	if handler, exists := f.handlers[providerType]; exists {
		return handler, nil
	}

	handlerFactory, exists := f.handlerFactories[providerType]
	if !exists {
		return nil, fmt.Errorf("unsupported handler for provider type: %s", providerType)
	}

	newHandler := handlerFactory(config)
	f.handlers[providerType] = newHandler
	return newHandler, nil
}

// GetSupportedProviders returns a list of supported provider types
func (f *DefaultProviderFactory) GetSupportedProviders() []provider.ProviderType {
	providers := make([]provider.ProviderType, 0, len(f.providers))
	for providerType := range f.providers {
		providers = append(providers, providerType)
	}
	return providers
}
