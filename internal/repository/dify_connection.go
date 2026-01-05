package repository

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DifyDatabaseConfig holds Dify database connection configuration
type DifyDatabaseConfig struct {
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

// LoadDifyDatabaseConfigFromEnv loads Dify database configuration from environment variables
func LoadDifyDatabaseConfigFromEnv() *DifyDatabaseConfig {
	config := &DifyDatabaseConfig{
		Host:            getEnvOrDefault("DIFY_DB_HOST", "localhost"),
		Port:            getEnvIntOrDefault("DIFY_DB_PORT", 5432),
		User:            getEnvOrDefault("DIFY_DB_USER", "postgres"),
		Password:        getEnvOrDefault("DIFY_DB_PASSWORD", ""),
		DBName:          getEnvOrDefault("DIFY_DB_NAME", "dify"),
		SSLMode:         getEnvOrDefault("DIFY_DB_SSLMODE", "disable"),
		MaxOpenConns:    getEnvIntOrDefault("DIFY_DB_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    getEnvIntOrDefault("DIFY_DB_MAX_IDLE_CONNS", 5),
		ConnMaxLifetime: time.Duration(getEnvIntOrDefault("DIFY_DB_CONN_MAX_LIFETIME_MINUTES", 30)) * time.Minute,
		ConnMaxIdleTime: time.Duration(getEnvIntOrDefault("DIFY_DB_CONN_MAX_IDLE_TIME_MINUTES", 5)) * time.Minute,
	}

	return config
}

// NewDifyDatabaseConnection creates a new GORM database connection to Dify database
func NewDifyDatabaseConnection(config *DifyDatabaseConfig) (*gorm.DB, error) {
	// Build connection string from individual parameters
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.User, config.Password, config.DBName, config.SSLMode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open Dify database: %w", err)
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

// DifyRepositoryManager manages repositories for Dify database
type DifyRepositoryManager struct {
	db           *gorm.DB
	apiTokenRepo *GormDifyApiTokenRepository
}

// NewDifyRepositoryManager creates a new Dify repository manager
func NewDifyRepositoryManager(db *gorm.DB) *DifyRepositoryManager {
	return &DifyRepositoryManager{
		db:           db,
		apiTokenRepo: NewGormDifyApiTokenRepository(db),
	}
}

// ApiToken returns the Dify API token repository
func (m *DifyRepositoryManager) ApiToken() DifyApiTokenRepository {
	return m.apiTokenRepo
}

// Ping checks the database connection
func (m *DifyRepositoryManager) Ping() error {
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

// Close closes the database connection
func (m *DifyRepositoryManager) Close() error {
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// NewDifyRepositoryManagerFromEnv creates a new Dify repository manager from environment variables
func NewDifyRepositoryManagerFromEnv() (*DifyRepositoryManager, error) {
	config := LoadDifyDatabaseConfigFromEnv()
	db, err := NewDifyDatabaseConnection(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Dify database connection: %w", err)
	}

	// Test the connection
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to ping Dify database: %w", err)
	}

	return NewDifyRepositoryManager(db), nil
}
