package repository

import (
	"context"

	"github.com/ClareAI/astra-voice-service/internal/domain"
	"gorm.io/gorm"
)

// VoiceTenantRepository defines the interface for voice tenant operations
type VoiceTenantRepository interface {
	// Create operations
	Create(ctx context.Context, req *domain.CreateVoiceTenantRequest) (*domain.VoiceTenant, error)

	// Read operations
	GetByID(ctx context.Context, id string) (*domain.VoiceTenant, error)
	GetByTenantID(ctx context.Context, tenantID string) (*domain.VoiceTenant, error)
	GetByAstraKey(ctx context.Context, astraKey string) (*domain.VoiceTenant, error)
	GetAll(ctx context.Context, includeDisabled bool) ([]*domain.VoiceTenant, error)
	GetWithAgents(ctx context.Context, id string) (*domain.VoiceTenantWithAgents, error)

	// Update operations
	Update(ctx context.Context, id string, req *domain.UpdateVoiceTenantRequest) (*domain.VoiceTenant, error)

	// Delete operations (soft delete)
	Delete(ctx context.Context, id string) error

	// Utility operations
	Exists(ctx context.Context, id string) (bool, error)
	ExistsByTenantID(ctx context.Context, tenantID string) (bool, error)
	ExistsByAstraKey(ctx context.Context, astraKey string) (bool, error)
}

// VoiceAgentRepository defines the interface for voice agent operations
type VoiceAgentRepository interface {
	// Create operations
	Create(ctx context.Context, req *domain.CreateVoiceAgentRequest) (*domain.VoiceAgent, error)

	// Read operations
	GetByID(ctx context.Context, id string) (*domain.VoiceAgent, error)
	GetByTenantID(ctx context.Context, tenantID string, includeDisabled bool) ([]*domain.VoiceAgent, error)
	GetByTenantIDAndTextAgentID(ctx context.Context, tenantID string, textAgentID string) (*domain.VoiceAgent, error)
	GetByTextAgentID(ctx context.Context, textAgentID string) (*domain.VoiceAgent, error)
	GetAll(ctx context.Context, includeDisabled bool) ([]*domain.VoiceAgent, error)

	// Update operations
	Update(ctx context.Context, id string, req *domain.UpdateVoiceAgentRequest) (*domain.VoiceAgent, error)
	PublishConfig(ctx context.Context, id string, config *domain.AgentConfigData) (*domain.VoiceAgent, error)

	// Delete operations (soft delete)
	Delete(ctx context.Context, id string) error

	// Utility operations
	Exists(ctx context.Context, id string) (bool, error)
	CountByTenantID(ctx context.Context, tenantID string) (int, error)

	// Platform voice agents operations (voice_agents table)
	GetAgentAPIKeyByPlatformAgentID(ctx context.Context, platformAgentID string, environment string) (string, error)
}

// RepositoryManager combines all repositories
type RepositoryManager interface {
	VoiceTenant() VoiceTenantRepository
	VoiceAgent() VoiceAgentRepository
	VoiceConversation() *VoiceConversationRepository
	VoiceMessage() *VoiceMessageRepository

	// Transaction support
	WithTx(ctx context.Context, fn func(ctx context.Context, repos RepositoryManager) error) error

	// Health check
	Ping(ctx context.Context) error

	// Close connection
	Close() error
}

// GormRepositoryManager implements RepositoryManager using GORM
type GormRepositoryManager struct {
	db                    *gorm.DB
	apiDB                 *gorm.DB
	voiceTenantRepo       *GormVoiceTenantRepository
	voiceAgentRepo        *GormVoiceAgentRepository
	voiceConversationRepo *VoiceConversationRepository
	voiceMessageRepo      *VoiceMessageRepository
}

// NewGormRepositoryManager creates a new GORM repository manager
// db: main database connection for voice_tenant and voice_agent
// apiDB: API database connection for voice_conversation and voice_message (can be nil, will fallback to main db)
func NewGormRepositoryManager(db *gorm.DB, apiDB *gorm.DB) *GormRepositoryManager {
	// Use apiDB for voice conversations and messages if provided, otherwise fallback to main db
	conversationDB := apiDB
	if conversationDB == nil {
		conversationDB = db
	}

	return &GormRepositoryManager{
		db:                    db,
		apiDB:                 apiDB,
		voiceTenantRepo:       NewGormVoiceTenantRepository(db),
		voiceAgentRepo:        NewGormVoiceAgentRepository(db),
		voiceConversationRepo: NewVoiceConversationRepository(conversationDB),
		voiceMessageRepo:      NewVoiceMessageRepository(conversationDB),
	}
}

// VoiceTenant returns the voice tenant repository
func (m *GormRepositoryManager) VoiceTenant() VoiceTenantRepository {
	return m.voiceTenantRepo
}

// VoiceAgent returns the voice agent repository
func (m *GormRepositoryManager) VoiceAgent() VoiceAgentRepository {
	return m.voiceAgentRepo
}

// VoiceConversation returns the voice conversation repository
func (m *GormRepositoryManager) VoiceConversation() *VoiceConversationRepository {
	return m.voiceConversationRepo
}

// VoiceMessage returns the voice message repository
func (m *GormRepositoryManager) VoiceMessage() *VoiceMessageRepository {
	return m.voiceMessageRepo
}

// WithTx executes a function within a database transaction
// Note: This only creates a transaction for the main database.
// API database operations will not be part of this transaction.
func (m *GormRepositoryManager) WithTx(ctx context.Context, fn func(ctx context.Context, repos RepositoryManager) error) error {
	return m.db.Transaction(func(tx *gorm.DB) error {
		// Use apiDB for conversations and messages if available, otherwise use transaction
		conversationDB := m.apiDB
		if conversationDB == nil {
			conversationDB = tx
		}

		txManager := &GormRepositoryManager{
			db:                    tx,
			apiDB:                 m.apiDB,
			voiceTenantRepo:       NewGormVoiceTenantRepository(tx),
			voiceAgentRepo:        NewGormVoiceAgentRepository(tx),
			voiceConversationRepo: NewVoiceConversationRepository(conversationDB),
			voiceMessageRepo:      NewVoiceMessageRepository(conversationDB),
		}
		return fn(ctx, txManager)
	})
}

// Ping checks the database connection
func (m *GormRepositoryManager) Ping(ctx context.Context) error {
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Close closes the database connection
func (m *GormRepositoryManager) Close() error {
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
