package repository

import (
	"fmt"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// APIDatabaseConfig holds API database connection configuration
type APIDatabaseConfig struct {
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

// LoadAPIDatabaseConfigFromEnv loads API database configuration from environment variables
func LoadAPIDatabaseConfigFromEnv() *APIDatabaseConfig {
	config := &APIDatabaseConfig{
		Host:            getEnvOrDefault("API_DB_HOST", "localhost"),
		Port:            getEnvIntOrDefault("API_DB_PORT", 5432),
		User:            getEnvOrDefault("API_DB_USER", "postgres"),
		Password:        getEnvOrDefault("API_DB_PASSWORD", ""),
		DBName:          getEnvOrDefault("API_DB_NAME", "api_db"),
		SSLMode:         getEnvOrDefault("API_DB_SSLMODE", "disable"),
		MaxOpenConns:    getEnvIntOrDefault("API_DB_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    getEnvIntOrDefault("API_DB_MAX_IDLE_CONNS", 5),
		ConnMaxLifetime: time.Duration(getEnvIntOrDefault("API_DB_CONN_MAX_LIFETIME_MINUTES", 30)) * time.Minute,
		ConnMaxIdleTime: time.Duration(getEnvIntOrDefault("API_DB_CONN_MAX_IDLE_TIME_MINUTES", 5)) * time.Minute,
	}

	return config
}

// NewAPIDatabaseConnection creates a new GORM API database connection
func NewAPIDatabaseConnection(config *APIDatabaseConfig) (*gorm.DB, error) {
	// Build connection string from individual parameters
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.User, config.Password, config.DBName, config.SSLMode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open API database: %w", err)
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB for API database: %w", err)
	}

	sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(config.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	return db, nil
}

// IsAPIDBConfigured checks if API database configuration is provided
func IsAPIDBConfigured() bool {
	return os.Getenv("API_DB_HOST") != ""
}
