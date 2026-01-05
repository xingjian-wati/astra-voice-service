package provider

// ModelHandler defines the interface for model handlers (supports multiple providers)
type ModelHandler interface {
	// InitializeConnectionWithLanguage initializes a model connection with specific language
	InitializeConnectionWithLanguage(connectionID, language, accent string) (ModelConnection, error)

	// SendInitialGreetingWithLanguage sends language-specific initial greeting
	SendInitialGreetingWithLanguage(connectionID, language string) error

	// GetCurrentLanguageAccent returns the current language and accent for a connection
	GetCurrentLanguageAccent(connectionID string) (string, string)

	// CloseConnection closes the model connection
	CloseConnection(connectionID string)

	// EnableGreetingSignalControl enables signal-based greeting control
	EnableGreetingSignalControl(connectionID string)

	// TriggerGreeting sends a signal to trigger the greeting
	TriggerGreeting(connectionID string)

	// IsGreetingSignalControlEnabled checks if signal control is enabled
	IsGreetingSignalControlEnabled(connectionID string) bool

	// SetOnConnectionClose sets the callback when connection is closed by logic
	SetOnConnectionClose(callback func(connectionID string))
}
