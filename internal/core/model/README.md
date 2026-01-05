# Model Provider Interface

This package provides a unified interface for supporting multiple AI model providers (OpenAI, Gemini, etc.) instead of being tightly coupled to OpenAI.

## Architecture

### Core Interfaces

1. **ModelProvider**: Defines the interface for different AI model providers
   - `InitializeConnection()`: Creates a connection to the provider
   - `GetProviderName()`: Returns provider identifier
   - `SupportsFeature()`: Checks feature support

2. **ModelConnection**: Represents an active connection to a model provider
   - `SendAudio()`: Sends audio data
   - `SendEvent()`: Sends events
   - `AddConversationHistory()`: Adds conversation history
   - `GenerateTTS()`: Triggers text-to-speech
   - `Close()`: Closes the connection
   - `IsConnected()`: Checks connection status

### Implementations

- **OpenAIProvider**: Implements ModelProvider for OpenAI Realtime API
- **GeminiProvider**: Placeholder for Google Gemini (not yet fully implemented)

### Handler

The `Handler` struct now uses `ModelProvider` interface instead of directly using OpenAI client:

```go
type Handler struct {
    provider    ModelProvider              // Model provider (OpenAI, Gemini, etc.)
    connections map[string]ModelConnection // Active connections
    // ... other fields
}
```

## Usage

### Creating a Handler with Default Provider (OpenAI)

```go
handler := model.NewHandler(cfg) // Uses OpenAI by default
```

### Creating a Handler with Custom Provider

```go
provider := model.NewOpenAIProvider(cfg)
handler := model.NewHandlerWithProvider(cfg, provider)
```

### Using Provider Factory

```go
factory := model.NewProviderFactory()
provider, err := factory.CreateProvider("openai", cfg)
if err != nil {
    // handle error
}
handler := model.NewHandlerWithProvider(cfg, provider)
```

## Adding a New Provider

To add support for a new provider (e.g., Gemini):

1. Implement `ModelProvider` interface:
   ```go
   type MyProvider struct {
       config *config.WebSocketConfig
   }
   
   func (p *MyProvider) GetProviderName() string {
       return "myprovider"
   }
   
   func (p *MyProvider) InitializeConnection(ctx context.Context, connectionID string, cfg *ConnectionConfig) (ModelConnection, error) {
       // Implementation
   }
   ```

2. Implement `ModelConnection` interface:
   ```go
   type MyConnection struct {
       // Provider-specific fields
   }
   
   func (c *MyConnection) SendAudio(samples []int16) error {
       // Implementation
   }
   // ... implement other methods
   ```

3. Register in ProviderFactory:
   ```go
   factory.RegisterProvider("myprovider", func(cfg *config.WebSocketConfig) ModelProvider {
       return NewMyProvider(cfg)
   })
   ```

## Backward Compatibility

The refactoring maintains backward compatibility:
- `InitializeConnection()` still returns `*webrtcadapter.Client` for OpenAI (via type assertion)
- `GetAIWebRTC()` method still works and falls back to `ModelConnection` if needed
- All existing code continues to work without changes

## Features

- **Multi-provider support**: Easy to add new providers
- **Feature detection**: Check if a provider supports specific features
- **Type-safe**: Uses Go interfaces for compile-time safety
- **Backward compatible**: Existing code continues to work

