package agent

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/cache"
	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/internal/repository"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/pkg/redis"
	"github.com/ClareAI/astra-voice-service/pkg/usage"
	"go.uber.org/zap"
)

// AgentService provides agent configuration management
type AgentService struct {
	agentCache     *cache.AgentCache
	repo           repository.RepositoryManager
	tenantAgentMap map[string][]string // tenant_id -> []agent_id mapping
	agentTenantMap map[string]string   // agent_id -> tenant_id mapping
	mutex          sync.RWMutex        // Protects tenant mappings and sync state
	syncInterval   time.Duration
	syncRunning    bool
	ctx            context.Context
	cancel         context.CancelFunc
	usageService   *usage.UsageService
}

// usageInitConfig centralizes usage-service related settings for easier testing/injection.
type usageInitConfig struct {
	BaseURL   string
	RedisConf *redis.RedisConfig
}

func loadUsageInitConfigFromEnv() usageInitConfig {
	baseURL := getEnvOrDefault("USAGE_BASE_URL", "")

	redisHost := getEnvOrDefault("REDIS_HOST", "localhost")
	redisPort := getEnvOrDefault("REDIS_PORT", "6379")
	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisDBStr := getEnvOrDefault("REDIS_DB", "0")
	redisDB, convErr := strconv.Atoi(redisDBStr)
	if convErr != nil {
		logger.Base().Error("Invalid REDIS_DB value, defaulting to 0", zap.String("redis_db_str", redisDBStr))
		redisDB = 0
	}

	return usageInitConfig{
		BaseURL: baseURL,
		RedisConf: &redis.RedisConfig{
			Host:     redisHost,
			Port:     redisPort,
			Password: redisPassword,
			DB:       redisDB,
		},
	}
}

// initUsageService wires usageService; returns error if configuration is present but initialization fails.
func (s *AgentService) initUsageService(cfg usageInitConfig) error {
	if cfg.BaseURL == "" {
		logger.Base().Info("Usage service URL not configured, usage checks disabled")
		return nil
	}
	if cfg.RedisConf == nil {
		return fmt.Errorf("redis config is required for usage service")
	}

	redisSvc, err := redis.NewRedisService(cfg.RedisConf)
	if err != nil {
		return fmt.Errorf("failed to init Redis for UsageService: %w", err)
	}

	s.usageService = usage.NewUsageService(cfg.BaseURL, redisSvc)
	logger.Base().Info("Usage service initialized", zap.String("base_url", cfg.BaseURL))
	return nil
}

var (
	agentServiceInstance *AgentService
	agentServiceOnce     sync.Once
	agentServiceMutex    sync.RWMutex
)

// GetAgentService returns the singleton instance of AgentService
func GetAgentService() (*AgentService, error) {
	var err error
	agentServiceOnce.Do(func() {
		agentServiceInstance, err = newAgentService()
	})
	return agentServiceInstance, err
}

// newAgentService creates a new agent service with HybridAgentFetcher and database sync (internal use only)
func newAgentService() (*AgentService, error) {
	usageCfg := loadUsageInitConfigFromEnv()

	repo, err := repository.NewRepositoryManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create repository manager: %w", err)
	}

	// Create context for the service lifecycle
	ctx, cancel := context.WithCancel(context.Background())

	service := &AgentService{
		agentCache:     cache.NewAgentCache(),
		repo:           repo,
		tenantAgentMap: make(map[string][]string),
		agentTenantMap: make(map[string]string),
		mutex:          sync.RWMutex{},
		syncInterval:   5 * time.Minute, // Default sync every 5 minutes
		syncRunning:    false,
		ctx:            ctx,
		cancel:         cancel,
	}

	// Initialize Usage service from config
	if err := service.initUsageService(usageCfg); err != nil {
		logger.Base().Error("Usage service init failed, continuing without usage checks")
	}

	// Initial load from database
	if err := service.LoadFromDatabase(ctx); err != nil {
		logger.Base().Warn("Failed to load initial data from database", zap.Error(err))
		// Don't return error here, allow service to start even if initial load fails
	}

	// Start periodic sync automatically
	service.startPeriodicSyncInternal()

	return service, nil
}

// NewAgentService creates a new agent service (deprecated: use GetAgentService instead)
// This function is kept for backward compatibility but now returns the singleton instance
func NewAgentService() (*AgentService, error) {
	return GetAgentService()
}

// ResetAgentService resets the singleton instance (mainly for testing)
func ResetAgentService() {
	agentServiceMutex.Lock()
	defer agentServiceMutex.Unlock()

	if agentServiceInstance != nil {
		agentServiceInstance.Close()
		agentServiceInstance = nil
	}
	agentServiceOnce = sync.Once{}
}

// ShutdownAgentService gracefully shuts down the singleton instance
func ShutdownAgentService() error {
	agentServiceMutex.Lock()
	defer agentServiceMutex.Unlock()

	if agentServiceInstance != nil {
		err := agentServiceInstance.Close()
		agentServiceInstance = nil
		agentServiceOnce = sync.Once{}
		return err
	}
	return nil
}

// startPeriodicSyncInternal starts the internal periodic sync goroutine
func (s *AgentService) startPeriodicSyncInternal() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.syncRunning {
		return // Already running
	}

	s.syncRunning = true

	go func() {
		defer func() {
			s.mutex.Lock()
			s.syncRunning = false
			s.mutex.Unlock()
		}()

		ticker := time.NewTicker(s.syncInterval)
		defer ticker.Stop()

		logger.Base().Info("✅ Started periodic database sync", zap.Duration("interval", s.syncInterval))

		for {
			select {
			case <-ticker.C:
				if err := s.LoadFromDatabase(s.ctx); err != nil {
					logger.Base().Error("❌ Periodic sync failed", zap.Error(err))
				} else {
					logger.Base().Info("✅ Periodic sync completed")
				}
			case <-s.ctx.Done():
				logger.Base().Info("🛑 Stopped periodic database sync (context cancelled)")
				return
			}
		}
	}()
}

// StartPeriodicSync starts periodic database synchronization (public method for manual control)
func (s *AgentService) StartPeriodicSync(ctx context.Context) error {
	if s.repo == nil {
		return fmt.Errorf("repository not available for sync")
	}

	s.startPeriodicSyncInternal()
	return nil
}

// SetSyncInterval sets the database sync interval (thread-safe)
func (s *AgentService) SetSyncInterval(interval time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.syncInterval = interval
	logger.Base().Info("🔄 Sync interval updated", zap.Duration("interval", interval))
}

// StopPeriodicSync stops periodic database synchronization
func (s *AgentService) StopPeriodicSync() {
	// Simply cancel the context, which will stop the sync goroutine
	if s.cancel != nil {
		s.cancel()
	}
}

// Close gracefully shuts down the agent service
func (s *AgentService) Close() error {
	// Cancel the context to stop all operations
	if s.cancel != nil {
		s.cancel()
	}

	// Stop periodic sync
	s.StopPeriodicSync()

	// Close repository if it has a close method
	if closer, ok := s.repo.(interface{ Close() error }); ok {
		return closer.Close()
	}

	return nil
}

// GetAgentConfigWithChannelType retrieves agent configuration based on ChannelType
// If ChannelType is ChannelTypeTest, returns Draft configuration; otherwise returns Published with Draft fallback
// If channelType is empty string, behaves the same as GetAgentConfig (Published with Draft fallback)
func (s *AgentService) GetAgentConfigWithChannelType(ctx context.Context, agentID string, channelType domain.ChannelType) (*config.AgentConfig, error) {
	// If channel type is test, use draft configuration
	if channelType == domain.ChannelTypeTest {
		return s.GetAgentDraft(ctx, agentID)
	}
	// Otherwise (including empty string), use published configuration with draft fallback
	return s.GetAgentConfig(ctx, agentID)
}

// GetAgentConfig retrieves agent configuration from HybridAgentFetcher (Published with Draft fallback)
func (s *AgentService) GetAgentConfig(ctx context.Context, agentID string) (*config.AgentConfig, error) {
	// 1. Initial Fetch (Resolve ID or TextAgentID) - Try to find Prod config directly
	// With Scheme B (Redundant Index), this will ONLY return Prod config (unless agentID already has suffix)
	cfg, err := s.agentCache.GetAgent(agentID)
	if err == nil {
		s.applyUsageFeatureFlags(ctx, cfg)
		return cfg, nil
	}

	// 2. Fallback to Draft (Virtual Prod)
	// If Prod not found, try looking up Draft version using suffix
	// With Scheme B, this works for both UUID+Suffix and TextID+Suffix
	draftCfg, err := s.agentCache.GetAgent(agentID + DraftConfigSuffix)
	if err == nil {
		// Create a copy to strip suffixes, making it look like a Prod config
		resultCfg := *draftCfg
		resultCfg.ID = strings.TrimSuffix(draftCfg.ID, DraftConfigSuffix)
		if resultCfg.TextAgentID != "" {
			resultCfg.TextAgentID = strings.TrimSuffix(draftCfg.TextAgentID, DraftConfigSuffix)
		}
		s.applyUsageFeatureFlags(ctx, &resultCfg)
		return &resultCfg, nil
	}

	return nil, fmt.Errorf("failed to get agent from factory: %w", err)
}

// GetAgentConfigByTenantID retrieves agent configurations for a tenant
func (s *AgentService) GetAgentConfigByTenantID(ctx context.Context, tenantID string) ([]*config.AgentConfig, error) {
	s.mutex.RLock()
	agentIDs, exists := s.tenantAgentMap[tenantID]
	s.mutex.RUnlock()

	if !exists {
		// Return empty slice if tenant has no agents
		return []*config.AgentConfig{}, nil
	}

	// Get agent configurations from HybridAgentFetcher
	var agentConfigs []*config.AgentConfig
	for _, agentID := range agentIDs {
		agentConfig, err := s.agentCache.GetAgent(agentID)
		if err != nil {
			// Log warning but continue with other agents
			logger.Base().Warn("Failed to get agent", zap.String("agent_id", agentID), zap.Error(err))
			continue
		}

		// Only include active agents
		if agentConfig.IsActive {
			agentConfigs = append(agentConfigs, agentConfig)
		}
	}

	return agentConfigs, nil
}

// GetTenantIDByAgentID retrieves the tenant ID for a given agent ID
func (s *AgentService) GetTenantIDByAgentID(agentID string) (string, error) {
	s.mutex.RLock()
	tenantID, exists := s.agentTenantMap[agentID]
	s.mutex.RUnlock()

	if !exists {
		return "", fmt.Errorf("agent %s not found or not associated with any tenant", agentID)
	}

	return tenantID, nil
}

// Check if tenant exists in the system
func (s *AgentService) TenantExists(tenantID string) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	_, exists := s.tenantAgentMap[tenantID]
	return exists
}

// ApplyFeatureFlags sets language/accent feature flags on an agent config based on the provided booleans.
// This mutates the given cfg pointer (callers should pass a copy if they need immutability).
func (s *AgentService) ApplyFeatureFlags(cfg *config.AgentConfig, langAllowed, accentAllowed bool) {
	if cfg == nil {
		return
	}

	apply := func(p *config.PromptConfig) {
		if p == nil {
			return
		}
		if !langAllowed {
			f := false
			p.AutoLanguageSwitching = &f
		}
		if !accentAllowed {
			f := false
			p.AutoAccentAdaptation = &f
		}
	}

	apply(cfg.PromptConfig)
	apply(cfg.OutboundPromptConfig)
}

// CheckTenantUsageAllowed checks usage for AI Agent category via the injected usage service.
func (s *AgentService) CheckTenantUsageAllowed(ctx context.Context, tenantID string) (bool, string) {
	if s.usageService == nil || tenantID == "" {
		return true, "Usage service not available"
	}
	allowed, msg, err := s.usageService.CheckUsageAllowed(ctx, tenantID, usage.UsageCategory_USAGE_CREDIT)
	if err != nil {
		logger.Base().Error("Usage check failed", zap.String("tenant_id", tenantID), zap.Error(err))
		return false, "Usage check failed"
	}
	return allowed, msg
}

// applyUsageFeatureFlags adjusts language/accent flags on cfg based on tenant usage config.
func (s *AgentService) applyUsageFeatureFlags(ctx context.Context, cfg *config.AgentConfig) {
	if cfg == nil || s.usageService == nil {
		return
	}

	baseID := strings.TrimSuffix(cfg.ID, DraftConfigSuffix)

	// Resolve tenant ID from cached mapping
	s.mutex.RLock()
	tenantID := s.agentTenantMap[baseID]
	s.mutex.RUnlock()

	if tenantID == "" {
		return
	}

	langAllowed := s.usageService.SupportsLanguageSwitch(ctx, tenantID)
	accentAllowed := s.usageService.SupportsAccentAdaptation(ctx, tenantID)

	s.ApplyFeatureFlags(cfg, langAllowed, accentAllowed)
}

// LoadFromDatabase loads agent configurations from database into HybridAgentFetcher and builds tenant mappings
func (s *AgentService) LoadFromDatabase(ctx context.Context) error {
	if s.repo == nil {
		return fmt.Errorf("repository not available")
	}

	// Get all agents from database (include disabled for complete mapping)
	voiceAgents, err := s.repo.VoiceAgent().GetAll(ctx, true) // include disabled
	if err != nil {
		return fmt.Errorf("failed to get agents from database: %w", err)
	}

	// Get all tenants to build tenant name mapping
	tenants, err := s.repo.VoiceTenant().GetAll(ctx, true) // include disabled
	if err != nil {
		logger.Base().Warn("Failed to get tenants from database", zap.Error(err))
	}

	// Build tenant ID to tenant name mapping
	// Note: Use TenantID (business ID) as key since VoiceTenantID now stores business IDs
	tenantNameMap := make(map[string]string)
	for _, tenant := range tenants {
		tenantNameMap[tenant.TenantID] = tenant.TenantName // Use business ID, not UUID
	}

	// Convert database agents to AgentConfig slice
	agentConfigs := make([]*config.AgentConfig, 0)

	// Lock for updating mappings
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Clear existing mappings
	s.tenantAgentMap = make(map[string][]string)
	s.agentTenantMap = make(map[string]string)

	// Convert agents and build tenant mappings
	for _, voiceAgent := range voiceAgents {
		// Only add active agents to the list for HybridAgentFetcher
		if !voiceAgent.Disabled {
			// 1. Convert to Prod Config (Key: AgentID)
			prodConfig := s.convertToProdAgentConfigWithTenant(voiceAgent, tenantNameMap)
			if prodConfig == nil {
				// No published config exists, create a "virtual Prod Config" using Draft data
				// This ensures textAgentIndex is populated for backward compatibility
				// Use convertConfigDataToConfig directly to preserve TextAgentID
				prodConfig = s.convertConfigDataToConfig(voiceAgent, voiceAgent.AgentConfig)
				prodConfig.ID = voiceAgent.ID // Canonical ID
				// TextAgentID is already set in convertConfigDataToConfig
				// Add tenant name
				if tenantName, exists := tenantNameMap[voiceAgent.VoiceTenantID]; exists {
					prodConfig.CompanyName = tenantName
				}
			}
			agentConfigs = append(agentConfigs, prodConfig)

			// 2. Convert to Draft Config (Key: AgentID:draft)
			draftConfig := s.convertToDraftAgentConfigWithTenant(voiceAgent, tenantNameMap)
			agentConfigs = append(agentConfigs, draftConfig)
		}

		// Build tenant mappings (for both active and disabled agents)
		tenantID := voiceAgent.VoiceTenantID
		agentID := voiceAgent.ID

		// Update tenant -> agents mapping
		if _, exists := s.tenantAgentMap[tenantID]; !exists {
			s.tenantAgentMap[tenantID] = make([]string, 0)
		}
		s.tenantAgentMap[tenantID] = append(s.tenantAgentMap[tenantID], agentID)

		// Update agent -> tenant mapping
		s.agentTenantMap[agentID] = tenantID
	}

	// Use UpdateAgentsAsync to bulk update AgentCache
	err = s.agentCache.UpdateAgentsAsync(agentConfigs)
	if err != nil {
		return fmt.Errorf("failed to update agents in cache: %w", err)
	}

	logger.Base().Info("✅ Updated AgentCache with active agents from database", zap.Int("agent_count", len(agentConfigs)), zap.String("method", "UpdateAgentsAsync"))
	logger.Base().Info("✅ Built tenant mappings", zap.Int("tenant_count", len(s.tenantAgentMap)), zap.Int("agent_count", len(s.agentTenantMap)))
	return nil
}

// convertToProdAgentConfigWithTenant converts to prod config with tenant info
func (s *AgentService) convertToProdAgentConfigWithTenant(voiceAgent *domain.VoiceAgent, tenantNameMap map[string]string) *config.AgentConfig {
	agentConfig := s.convertToProdAgentConfig(voiceAgent)
	if agentConfig == nil {
		return nil
	}
	if tenantName, exists := tenantNameMap[voiceAgent.VoiceTenantID]; exists {
		agentConfig.CompanyName = tenantName
	}
	return agentConfig
}

// convertToDraftAgentConfigWithTenant converts to draft config with tenant info
func (s *AgentService) convertToDraftAgentConfigWithTenant(voiceAgent *domain.VoiceAgent, tenantNameMap map[string]string) *config.AgentConfig {
	agentConfig := s.convertToDraftAgentConfig(voiceAgent)
	if tenantName, exists := tenantNameMap[voiceAgent.VoiceTenantID]; exists {
		agentConfig.CompanyName = tenantName
	}
	return agentConfig
}

// DraftConfigSuffix is the suffix used for draft agent configuration keys in cache
const DraftConfigSuffix = ":" + config.AgentConfigModeDraft

// convertToProdAgentConfig converts repository.VoiceAgent to config.AgentConfig (Production)
// Returns nil if no PublishedAgentConfig exists
func (s *AgentService) convertToProdAgentConfig(voiceAgent *domain.VoiceAgent) *config.AgentConfig {
	src := voiceAgent.PublishedAgentConfig
	if src == nil {
		return nil // No published config
	}
	cfg := s.convertConfigDataToConfig(voiceAgent, src)
	cfg.ID = voiceAgent.ID
	return cfg
}

// convertToDraftAgentConfig converts repository.VoiceAgent to config.AgentConfig (Draft)
// Always uses AgentConfig (Draft)
func (s *AgentService) convertToDraftAgentConfig(voiceAgent *domain.VoiceAgent) *config.AgentConfig {
	src := voiceAgent.AgentConfig
	cfg := s.convertConfigDataToConfig(voiceAgent, src)
	cfg.ID = voiceAgent.ID + DraftConfigSuffix
	// Redundant TextAgentID with suffix to simplify lookup and support draft-only agents
	if cfg.TextAgentID != "" {
		cfg.TextAgentID = cfg.TextAgentID + DraftConfigSuffix
	}
	return cfg
}

// convertConfigDataToConfig converts specific config data to config.AgentConfig
func (s *AgentService) convertConfigDataToConfig(voiceAgent *domain.VoiceAgent, configData *domain.AgentConfigData) *config.AgentConfig {
	agentConfig := &config.AgentConfig{
		ID:          voiceAgent.ID,
		Name:        voiceAgent.AgentName,
		CompanyName: "", // Will be filled from tenant info if needed
		Industry:    "",
		Description: "",
		IsActive:    !voiceAgent.Disabled,
		CreatedAt:   voiceAgent.CreatedAt,
		UpdatedAt:   voiceAgent.UpdatedAt,
	}

	// Copy TextAgentID if present
	if voiceAgent.TextAgentID != nil {
		agentConfig.TextAgentID = *voiceAgent.TextAgentID
	}

	// Convert agent config data if present
	if configData != nil {
		agentConfig.Persona = configData.Persona
		agentConfig.Services = configData.Services
		agentConfig.Tone = configData.Tone
		agentConfig.Language = configData.Language
		agentConfig.DefaultAccent = configData.DefaultAccent
		agentConfig.Expertise = configData.Expertise
		agentConfig.Voice = configData.Voice
		agentConfig.Speed = configData.Speed
		agentConfig.BusinessNumber = configData.BusinessNumber

		// Set default MaxCallDuration if not explicitly configured (0)
		if configData.MaxCallDuration > 0 {
			agentConfig.MaxCallDuration = configData.MaxCallDuration
		} else {
			agentConfig.MaxCallDuration = 300 // Default 5 minutes (300 seconds)
		}

		// Convert silence config
		if configData.SilenceConfig != nil {
			agentConfig.SilenceConfig = &config.SilenceConfig{
				InactivityCheckDuration: configData.SilenceConfig.InactivityCheckDuration,
				MaxRetries:              configData.SilenceConfig.MaxRetries,
				InactivityMessage:       configData.SilenceConfig.InactivityMessage,
			}
			agentConfig.SilenceConfig.SetDefaults()
		} else {
			agentConfig.SilenceConfig = &config.SilenceConfig{}
			agentConfig.SilenceConfig.SetDefaults()
		}

		// Convert prompt config
		if configData.PromptConfig != nil {
			agentConfig.PromptConfig = &config.PromptConfig{
				GreetingTemplate:      configData.PromptConfig.GreetingTemplate,
				RealtimeTemplate:      configData.PromptConfig.RealtimeTemplate,
				SystemInstructions:    configData.PromptConfig.SystemInstructions,
				ConversationFlow:      configData.PromptConfig.ConversationFlow,
				ExampleDialogues:      configData.PromptConfig.ExampleDialogues,
				LanguageInstructions:  configData.PromptConfig.LanguageInstructions,
				CustomVariables:       configData.PromptConfig.CustomVariables,
				AutoLanguageSwitching: configData.PromptConfig.AutoLanguageSwitching,
				AutoAccentAdaptation:  configData.PromptConfig.AutoAccentAdaptation,
			}
		}

		// Convert RAG config
		if configData.RAGConfig != nil {
			agentConfig.RAGConfig = &config.RAGConfig{
				Enabled:     configData.RAGConfig.Enabled,
				BaseURL:     configData.RAGConfig.BaseURL,
				Token:       configData.RAGConfig.Token,
				WorkflowID:  configData.RAGConfig.WorkflowID,
				Headers:     configData.RAGConfig.Headers,
				Description: configData.RAGConfig.Description,
				Timeout:     configData.RAGConfig.Timeout,
				MaxRetries:  configData.RAGConfig.MaxRetries,
			}
		}

		// Convert API config
		if configData.APIConfig != nil {
			agentConfig.APIConfig = &config.APIConfig{
				Endpoints: configData.APIConfig.Endpoints,
				Tokens:    configData.APIConfig.Tokens,
				Headers:   configData.APIConfig.Headers,
			}
		}

		// Convert business rules
		if configData.BusinessRules != nil {
			agentConfig.BusinessRules = &config.BusinessRules{
				AllowedActions:      configData.BusinessRules.AllowedActions,
				RequiredFields:      configData.BusinessRules.RequiredFields,
				ValidationRules:     configData.BusinessRules.ValidationRules,
				MaxConversationTime: configData.BusinessRules.MaxConversationTime,
			}

			// Convert working hours
			if configData.BusinessRules.WorkingHours != nil {
				agentConfig.BusinessRules.WorkingHours = &config.WorkingHours{
					Timezone: configData.BusinessRules.WorkingHours.Timezone,
					Schedule: configData.BusinessRules.WorkingHours.Schedule,
				}
			}

			// Convert escalation rules
			if configData.BusinessRules.EscalationRules != nil {
				var escalationRules []config.EscalationRule
				for _, rule := range configData.BusinessRules.EscalationRules {
					escalationRules = append(escalationRules, config.EscalationRule{
						Condition: rule.Condition,
						Action:    rule.Action,
						Target:    rule.Target,
					})
				}
				agentConfig.BusinessRules.EscalationRules = escalationRules
			}

			// Convert function call rules
			if configData.BusinessRules.FunctionCallRules != nil {
				functionCallRules := make(map[string]*config.FunctionRule)
				for functionName, rule := range configData.BusinessRules.FunctionCallRules {
					functionCallRules[functionName] = &config.FunctionRule{
						Description: rule.Description,
						When:        rule.When,
						Parameters:  rule.Parameters,
					}
				}
				agentConfig.BusinessRules.FunctionCallRules = functionCallRules
			}
		}

		// Convert IntegratedActions
		if configData.IntegratedActions != nil {
			agentConfig.IntegratedActions = configData.IntegratedActions
		}

		// Convert OutboundPromptConfig
		if configData.OutboundPromptConfig != nil {
			agentConfig.OutboundPromptConfig = &config.PromptConfig{
				GreetingTemplate:      configData.OutboundPromptConfig.GreetingTemplate,
				RealtimeTemplate:      configData.OutboundPromptConfig.RealtimeTemplate,
				SystemInstructions:    configData.OutboundPromptConfig.SystemInstructions,
				ConversationFlow:      configData.OutboundPromptConfig.ConversationFlow,
				ExampleDialogues:      configData.OutboundPromptConfig.ExampleDialogues,
				LanguageInstructions:  configData.OutboundPromptConfig.LanguageInstructions,
				CustomVariables:       configData.OutboundPromptConfig.CustomVariables,
				AutoLanguageSwitching: configData.OutboundPromptConfig.AutoLanguageSwitching,
				AutoAccentAdaptation:  configData.OutboundPromptConfig.AutoAccentAdaptation,
			}
		}

		// Convert OutboundIntegratedActions
		if configData.OutboundIntegratedActions != nil {
			agentConfig.OutboundIntegratedActions = configData.OutboundIntegratedActions
		}
	}

	// Handle instruction field - use it as system instructions if prompt config doesn't exist
	if voiceAgent.Instruction != nil && *voiceAgent.Instruction != "" {
		if agentConfig.PromptConfig == nil {
			agentConfig.PromptConfig = &config.PromptConfig{}
		}
		if agentConfig.PromptConfig.SystemInstructions == "" {
			agentConfig.PromptConfig.SystemInstructions = *voiceAgent.Instruction
		}
	}

	ensureDefaultAccent(agentConfig)

	return agentConfig
}

func ensureDefaultAccent(agentConfig *config.AgentConfig) {
	if agentConfig == nil || agentConfig.Language == "" || agentConfig.DefaultAccent == "" {
		return
	}

	if agentConfig.PromptConfig != nil {
		ensurePromptConfigHasAccent(agentConfig.PromptConfig, agentConfig.Language, agentConfig.DefaultAccent)
	}
	if agentConfig.OutboundPromptConfig != nil {
		ensurePromptConfigHasAccent(agentConfig.OutboundPromptConfig, agentConfig.Language, agentConfig.DefaultAccent)
	}
}

func ensurePromptConfigHasAccent(prompt *config.PromptConfig, language, accent string) {
	if prompt == nil || language == "" || accent == "" {
		return
	}

	if prompt.LanguageInstructions == nil {
		prompt.LanguageInstructions = make(map[string]string)
	}

	if prompt.IsAutoAccentAdaptationEnabled() {
		if _, exists := prompt.LanguageInstructions[language]; exists {
			prompt.LanguageInstructions[language] = appendAccentValue(prompt.LanguageInstructions[language], accent)
		}
		return
	}

	prompt.LanguageInstructions[language] = appendAccentValue(prompt.LanguageInstructions[language], accent)
}

func appendAccentValue(existing, accent string) string {
	accent = strings.TrimSpace(accent)
	if accent == "" {
		return existing
	}

	if existing == "" {
		return accent
	}

	parts := strings.Split(existing, ",")
	seen := make(map[string]bool)
	var normalized []string

	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if !seen[key] {
			seen[key] = true
			normalized = append(normalized, p)
		}
	}

	accentKey := strings.ToLower(accent)
	if !seen[accentKey] {
		normalized = append(normalized, accent)
	}

	return strings.Join(normalized, ",")
}

// SyncAgentCreate synchronizes a newly created agent to the cache
func (s *AgentService) SyncAgentCreate(ctx context.Context, voiceAgent *domain.VoiceAgent) error {
	if voiceAgent == nil {
		return fmt.Errorf("voice agent cannot be nil")
	}

	// Upsert Prod Config in cache (if published, or create virtual Prod for backward compatibility)
	prodConfig := s.convertToProdAgentConfig(voiceAgent)
	if prodConfig == nil {
		// No published config, create virtual Prod Config using Draft data
		prodConfig = s.convertConfigDataToConfig(voiceAgent, voiceAgent.AgentConfig)
		prodConfig.ID = voiceAgent.ID // Canonical ID
		// TextAgentID is already set in convertConfigDataToConfig
	}
	if err := s.agentCache.UpsertAgent(prodConfig); err != nil {
		return fmt.Errorf("failed to upsert prod agent in cache: %w", err)
	}

	// Upsert Draft Config in cache
	draftConfig := s.convertToDraftAgentConfig(voiceAgent)
	if err := s.agentCache.UpsertAgent(draftConfig); err != nil {
		return fmt.Errorf("failed to upsert draft agent in cache: %w", err)
	}

	// Update tenant mappings
	s.mutex.Lock()
	s.agentTenantMap[voiceAgent.ID] = voiceAgent.VoiceTenantID
	if _, exists := s.tenantAgentMap[voiceAgent.VoiceTenantID]; !exists {
		s.tenantAgentMap[voiceAgent.VoiceTenantID] = []string{}
	}
	// Check if agent already in tenant's list to avoid duplicates
	found := false
	for _, agentID := range s.tenantAgentMap[voiceAgent.VoiceTenantID] {
		if agentID == voiceAgent.ID {
			found = true
			break
		}
	}
	if !found {
		s.tenantAgentMap[voiceAgent.VoiceTenantID] = append(s.tenantAgentMap[voiceAgent.VoiceTenantID], voiceAgent.ID)
	}
	s.mutex.Unlock()

	logger.Base().Info("✅ Synced agent to cache", zap.String("agent_name", voiceAgent.AgentName), zap.String("agent_id", voiceAgent.ID))
	return nil
}

// SyncAgentUpdate synchronizes an updated agent to the cache
func (s *AgentService) SyncAgentUpdate(ctx context.Context, voiceAgent *domain.VoiceAgent) error {
	if voiceAgent == nil {
		return fmt.Errorf("voice agent cannot be nil")
	}

	// Upsert Prod Config in cache (if published, or create virtual Prod for backward compatibility)
	prodConfig := s.convertToProdAgentConfig(voiceAgent)
	if prodConfig == nil {
		// No published config, create virtual Prod Config using Draft data
		prodConfig = s.convertConfigDataToConfig(voiceAgent, voiceAgent.AgentConfig)
		prodConfig.ID = voiceAgent.ID // Canonical ID
		// TextAgentID is already set in convertConfigDataToConfig
	}
	if err := s.agentCache.UpsertAgent(prodConfig); err != nil {
		return fmt.Errorf("failed to upsert prod agent in cache: %w", err)
	}

	// Upsert Draft Config in cache
	draftConfig := s.convertToDraftAgentConfig(voiceAgent)
	if err := s.agentCache.UpsertAgent(draftConfig); err != nil {
		return fmt.Errorf("failed to upsert draft agent in cache: %w", err)
	}

	// Update tenant mappings if tenant changed
	s.mutex.Lock()
	oldTenantID, exists := s.agentTenantMap[voiceAgent.ID]
	if exists && oldTenantID != voiceAgent.VoiceTenantID {
		// Remove from old tenant
		if agentIDs, ok := s.tenantAgentMap[oldTenantID]; ok {
			newAgentIDs := make([]string, 0, len(agentIDs))
			for _, id := range agentIDs {
				if id != voiceAgent.ID {
					newAgentIDs = append(newAgentIDs, id)
				}
			}
			s.tenantAgentMap[oldTenantID] = newAgentIDs
		}
	}
	// Add to new tenant
	s.agentTenantMap[voiceAgent.ID] = voiceAgent.VoiceTenantID
	if _, ok := s.tenantAgentMap[voiceAgent.VoiceTenantID]; !ok {
		s.tenantAgentMap[voiceAgent.VoiceTenantID] = []string{}
	}
	// Check if already in list
	found := false
	for _, id := range s.tenantAgentMap[voiceAgent.VoiceTenantID] {
		if id == voiceAgent.ID {
			found = true
			break
		}
	}
	if !found {
		s.tenantAgentMap[voiceAgent.VoiceTenantID] = append(s.tenantAgentMap[voiceAgent.VoiceTenantID], voiceAgent.ID)
	}
	s.mutex.Unlock()

	logger.Base().Info("✅ Synced agent update to cache", zap.String("agent_name", voiceAgent.AgentName), zap.String("agent_id", voiceAgent.ID))
	return nil
}

// SyncAgentDelete synchronizes an agent deletion to the cache
func (s *AgentService) SyncAgentDelete(ctx context.Context, agentID string, tenantID string) error {
	if agentID == "" {
		return fmt.Errorf("agent ID cannot be empty")
	}

	// Delete Prod Config from cache
	if err := s.agentCache.DeleteAgent(agentID); err != nil {
		// Log warning but don't fail if agent doesn't exist
		logger.Base().Warn("⚠️  Failed to delete prod agent from cache", zap.String("agent_id", agentID), zap.Error(err))
	}

	// Delete Draft Config from cache
	if err := s.agentCache.DeleteAgent(agentID + DraftConfigSuffix); err != nil {
		logger.Base().Warn("⚠️  Failed to delete draft agent from cache", zap.String("agent_id", agentID+DraftConfigSuffix), zap.Error(err))
	}

	// Remove from tenant mappings
	s.mutex.Lock()
	delete(s.agentTenantMap, agentID)
	if agentIDs, exists := s.tenantAgentMap[tenantID]; exists {
		newAgentIDs := make([]string, 0, len(agentIDs))
		for _, id := range agentIDs {
			if id != agentID {
				newAgentIDs = append(newAgentIDs, id)
			}
		}
		s.tenantAgentMap[tenantID] = newAgentIDs
	}
	s.mutex.Unlock()

	logger.Base().Info("✅ Synced agent deletion to cache", zap.String("agent_id", agentID))
	return nil
}

// GetAgentDraft retrieves the draft configuration for an agent
// Supports both AgentID (UUID) and TextAgentID as input
func (s *AgentService) GetAgentDraft(ctx context.Context, idOrTextID string) (*config.AgentConfig, error) {
	// Since we now index Draft TextAgentIDs with suffix, we can just query directly with suffix
	// This handles both UUID+Suffix and TextID+Suffix cases uniformly
	// Even if Prod doesn't exist, this will work because Draft index is independent
	draftCfg, err := s.agentCache.GetAgent(idOrTextID + DraftConfigSuffix)
	if err != nil {
		return nil, fmt.Errorf("failed to get draft config: %w", err)
	}

	// Return a copy to avoid modifying the cache
	// Restore Canonical ID and TextAgentID (remove suffix) for the caller
	// We must create a new struct to avoid modifying the cached object (although GetAgent returns a copy, let's be safe)
	resultCfg := *draftCfg // Shallow copy is fine as we only modify strings

	resultCfg.ID = strings.TrimSuffix(draftCfg.ID, DraftConfigSuffix)
	if resultCfg.TextAgentID != "" {
		resultCfg.TextAgentID = strings.TrimSuffix(draftCfg.TextAgentID, DraftConfigSuffix)
	}

	s.applyUsageFeatureFlags(ctx, &resultCfg)
	return &resultCfg, nil
}

// Implementation of config.AgentFetcher interface

// GetAgent implements AgentFetcher interface (Prod with Draft fallback)
func (s *AgentService) GetAgent(id string) (*config.AgentConfig, error) {
	return s.GetAgentConfig(context.Background(), id)
}

// GetAllAgents implements AgentFetcher interface
// Returns effective configuration for all agents (Prod > Draft)
func (s *AgentService) GetAllAgents() ([]*config.AgentConfig, error) {
	all, err := s.agentCache.GetAllAgents()
	if err != nil {
		return nil, err
	}

	// Deduplicate and prioritize Prod over Draft
	agentMap := make(map[string]*config.AgentConfig)

	// First pass: Collect Prod configs
	for _, cfg := range all {
		if !strings.HasSuffix(cfg.ID, DraftConfigSuffix) {
			agentMap[cfg.ID] = cfg
		}
	}

	// Second pass: Collect Draft configs for agents without Prod config
	for _, cfg := range all {
		if strings.HasSuffix(cfg.ID, DraftConfigSuffix) {
			baseID := strings.TrimSuffix(cfg.ID, DraftConfigSuffix)
			if _, exists := agentMap[baseID]; !exists {
				cfg.ID = baseID // Fix ID
				agentMap[baseID] = cfg
			}
		}
	}

	result := make([]*config.AgentConfig, 0, len(agentMap))
	for _, cfg := range agentMap {
		result = append(result, cfg)
	}
	return result, nil
}

// GetActiveAgents implements AgentFetcher interface
func (s *AgentService) GetActiveAgents() ([]*config.AgentConfig, error) {
	all, err := s.GetAllAgents()
	if err != nil {
		return nil, err
	}

	var active []*config.AgentConfig
	for _, cfg := range all {
		if cfg.IsActive {
			active = append(active, cfg)
		}
	}
	return active, nil
}

// ListAgentIDs implements AgentFetcher interface
func (s *AgentService) ListAgentIDs() ([]string, error) {
	all, err := s.GetAllAgents()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(all))
	for _, cfg := range all {
		ids = append(ids, cfg.ID)
	}
	return ids, nil
}

// getEnvOrDefault returns the environment variable value or the provided default if empty.
func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
