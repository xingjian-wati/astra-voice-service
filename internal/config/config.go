package config

// Removed unused imports: log, godotenv

// Config holds the application configuration
type Config struct {
	AccountServiceEndpoint         string
	APIServiceEndpoint             string
	RAGApiServiceInternalEndpoint  string
	UsageAPIServiceEndpoint        string
	MappingServiceGRPCEndpoint     string
	IntegrationServiceGRPCEndpoint string
	// Add other configuration fields as needed
}

var (
	// DefaultConfig holds the default configuration values
	DefaultConfig = Config{
		AccountServiceEndpoint:         "localhost:50051",
		APIServiceEndpoint:             "localhost:8004",
		RAGApiServiceInternalEndpoint:  "localhost:8006",
		UsageAPIServiceEndpoint:        "localhost:8001",
		MappingServiceGRPCEndpoint:     "localhost:8003",
		IntegrationServiceGRPCEndpoint: "localhost:8003",
		// Set other default values
	}

	// AppConfig holds the current configuration
	AppConfig Config
)

// LoadConfig loads configuration from environment variables
func LoadConfig() {
	// Note: .env file is loaded in main.go for local development using godotenv.Load()

	// Refresh default tenant IDs from environment (now that .env is loaded)
	RefreshDefaults()

	// Override with environment variables
	AppConfig = DefaultConfig

	// Override other configuration fields as needed
}

// GetConfig returns the current configuration
func GetConfig() Config {
	return AppConfig
}
