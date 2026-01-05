package repository

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DatabaseConfig holds database connection configuration
type DatabaseConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	DBName          string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// LoadDatabaseConfigFromEnv loads database configuration from environment variables
func LoadDatabaseConfigFromEnv() *DatabaseConfig {
	config := &DatabaseConfig{
		Host:            getEnvOrDefault("DB_HOST", "localhost"),
		Port:            getEnvIntOrDefault("DB_PORT", 5432),
		User:            getEnvOrDefault("DB_USER", "postgres"),
		Password:        getEnvOrDefault("DB_PASSWORD", ""),
		DBName:          getEnvOrDefault("DB_NAME", "voice_gateway"),
		SSLMode:         getEnvOrDefault("DB_SSLMODE", "disable"),
		MaxOpenConns:    getEnvIntOrDefault("DB_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    getEnvIntOrDefault("DB_MAX_IDLE_CONNS", 5),
		ConnMaxLifetime: time.Duration(getEnvIntOrDefault("DB_CONN_MAX_LIFETIME_MINUTES", 30)) * time.Minute,
		ConnMaxIdleTime: time.Duration(getEnvIntOrDefault("DB_CONN_MAX_IDLE_TIME_MINUTES", 5)) * time.Minute,
	}

	return config
}

// NewDatabaseConnection creates a new GORM database connection
func NewDatabaseConnection(config *DatabaseConfig) (*gorm.DB, error) {
	// Build connection string from individual parameters
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.User, config.Password, config.DBName, config.SSLMode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(config.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	return db, nil
}

// AutoMigrate runs database migrations for all models
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&domain.VoiceTenant{},
		&domain.VoiceAgent{},
		&domain.VoiceConversation{},
		&domain.VoiceMessage{},
	)
}

// AutoMigrateAPIDB runs database migrations for API database models only
// This is used when a separate API database is configured for voice_conversations and voice_messages
func AutoMigrateAPIDB(db *gorm.DB) error {
	return db.AutoMigrate(
		&domain.VoiceConversation{},
		&domain.VoiceMessage{},
	)
}

// NewRepositoryManager creates a new repository manager with database connections
func NewRepositoryManager() (RepositoryManager, error) {
	// Create main database connection
	config := LoadDatabaseConfigFromEnv()
	db, err := NewDatabaseConnection(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create database connection: %w", err)
	}

	// Test the main connection
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if err := AutoMigrate(db); err != nil {
		return nil, fmt.Errorf("failed to run auto migration: %w", err)
	}

	// Create API database connection if configured
	var apiDB *gorm.DB
	if IsAPIDBConfigured() {
		apiConfig := LoadAPIDatabaseConfigFromEnv()
		apiDB, err = NewAPIDatabaseConnection(apiConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create API database connection: %w", err)
		}

		// Test the API connection
		apiSqlDB, err := apiDB.DB()
		if err != nil {
			return nil, fmt.Errorf("failed to get underlying sql.DB for API database: %w", err)
		}

		if err := apiSqlDB.Ping(); err != nil {
			apiSqlDB.Close()
			return nil, fmt.Errorf("failed to ping API database: %w", err)
		}

		// Run migrations for API database (VoiceConversation and VoiceMessage tables)
		if err := AutoMigrateAPIDB(apiDB); err != nil {
			return nil, fmt.Errorf("failed to run API database auto migration: %w", err)
		}
	}

	return NewGormRepositoryManager(db, apiDB), nil
}

// getEnvOrDefault gets environment variable or returns default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvIntOrDefault gets environment variable as int or returns default value
func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
