package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ClareAI/astra-protocol/api/common"
	"github.com/ClareAI/astra-protocol/api/event"
	usage "github.com/ClareAI/astra-protocol/api/usage-service"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/pkg/redis"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
)

type UsageService struct {
	BaseURL      string
	Client       *http.Client
	redisService redis.RedisServiceInterface
}
type UsageInfo struct {
	Category       UsageCategory `json:"category"`
	UsedAmount     int64         `json:"used_amount"`
	TotalAmount    int64         `json:"total_amount"`
	ExpirationTime time.Time     `json:"expiration_time"`
}

type SubscriptionPlan int32

const (
	SubscriptionPlan_FREE       SubscriptionPlan = 0
	SubscriptionPlan_PRO        SubscriptionPlan = 1
	SubscriptionPlan_ENTERPRISE SubscriptionPlan = 2
	SubscriptionPlan_CUSTOM     SubscriptionPlan = 3
)

type UsageCategory int32

const (
	UsageCategory_USAGE_UNKNOWN     UsageCategory = 0
	UsageCategory_USAGE_CREDIT      UsageCategory = 1
	UsageCategory_USAGE_AI_AGENT    UsageCategory = 2
	UsageCategory_USAGE_INTEGRATION UsageCategory = 3
	UsageCategory_USAGE_AGENT_KB    UsageCategory = 4
)

func NewUsageService(baseURL string, redisService redis.RedisServiceInterface) *UsageService {
	return &UsageService{
		BaseURL: baseURL,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		redisService: redisService,
	}
}

func (s *UsageService) GetTenantCurrentUsage(ctx context.Context, tenantID string) (*usage.TenantCurrentUsageResponse, error) {
	url := fmt.Sprintf("%s/usage/v1/usage/%s", s.BaseURL, tenantID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response usage.TenantCurrentUsageResponse
	// Be tolerant of unknown enums/fields from server to avoid failing usage gate.
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &response, nil
}

// CheckUsageAllowed checks if a specific usage type is allowed for the tenant
func (s *UsageService) CheckUsageAllowed(ctx context.Context, tenantID string, category UsageCategory) (bool, string, error) {
	categoryName := s.getCategoryName(category)
	zap.L().Info("Starting usage check",
		zap.String("tenant_id", tenantID),
		zap.String("category", categoryName),
		zap.Int32("category_code", int32(category)))

	// First try to get from Redis cache
	var usageConfig string
	if s.redisService != nil {
		key := s.redisService.GenerateKey(redis.USAGE_CONFIG, tenantID)
		logger.Base().Info("key", zap.String("key", key))

		val, err := s.redisService.GetValue(ctx, key)
		if err != nil && err != redis.ErrKeyNotExist {
			logger.Base().Error("failed to get tenant usage config")
			zap.L().Error("Failed to get tenant usage config from cache",
				zap.String("tenant_id", tenantID),
				zap.String("category", categoryName),
				zap.Error(err))
			// Treat as cache miss and fall back to source.
		}
		usageConfig = val
		logger.Base().Info("usage config", zap.String("usageconfig", usageConfig))
	}

	oc := &event.TenantUsageConfiguration{}
	expired := true

	if usageConfig != "" {
		if err := json.Unmarshal([]byte(usageConfig), oc); err != nil {
			logger.Base().Error("failed to unmarshal tenant usage config (treat as miss)")
		} else {
			logger.Base().Info("JSON usage config", zap.Any("config", oc))
			if !oc.VoiceAgent {
				return false, "Voice agent is not enabled for your current plan.", nil
			}

			// Check if the specified category usage config is not expired
			for _, uc := range oc.UsageConfigs {
				if uc.Category == common.UsageCategory(category) {
					if uc.ExpirationTime.AsTime().After(time.Now()) {
						expired = false
						// Check if user has available quota
						if uc.TotalAmount == -1 || uc.TotalAmount-uc.UsedAmount > 0 {
							zap.L().Info("Usage check passed - quota available from cache",
								zap.String("tenant_id", tenantID),
								zap.String("category", categoryName),
								zap.Int64("used_amount", uc.UsedAmount),
								zap.Int64("total_amount", uc.TotalAmount),
								zap.Bool("unlimited", uc.TotalAmount == -1))
							return true, "", nil
						}
						// If not expired but no quota available, check plan limits
						zap.L().Warn("Usage check failed - quota exceeded from cache",
							zap.String("tenant_id", tenantID),
							zap.String("category", categoryName),
							zap.Int64("used_amount", uc.UsedAmount),
							zap.Int64("total_amount", uc.TotalAmount))
						break
					}
					break
				}
			}
		}
	}

	// If expired or not found in cache, fetch from usage service
	if expired || usageConfig == "" {
		zap.L().Info("Cache expired or not found, fetching from usage service",
			zap.String("tenant_id", tenantID),
			zap.String("category", categoryName),
			zap.Bool("expired", expired),
			zap.Bool("config_empty", usageConfig == ""))

		response, err := s.GetTenantCurrentUsage(ctx, tenantID)
		if err != nil {
			zap.L().Error("Failed to get tenant usage from service",
				zap.String("tenant_id", tenantID),
				zap.String("category", categoryName),
				zap.Error(err))
			return false, "", fmt.Errorf("failed to get tenant usage: %w", err)
		}

		// Check usage limits using the fresh config
		if response.Config != nil && !response.Config.VoiceAgent {
			return false, "Voice agent is not enabled for your current plan.", nil
		}
		allowed, message, checkErr := s.checkUsageLimits(response.Config, category)
		zap.L().Info("Usage check completed from service",
			zap.String("tenant_id", tenantID),
			zap.String("category", categoryName),
			zap.Bool("allowed", allowed),
			zap.String("message", message),
			zap.Error(checkErr))
		return allowed, message, checkErr
	}

	// If no specific config found, return not allowed with appropriate message
	zap.L().Warn("Usage check failed - no configuration found",
		zap.String("tenant_id", tenantID),
		zap.String("category", categoryName))
	return false, fmt.Sprintf("%s feature is not available or configured for your current plan.", categoryName), nil
}

// getTenantUsageConfig retrieves tenant usage configuration from cache or usage service.
func (s *UsageService) getTenantUsageConfig(ctx context.Context, tenantID string) (*event.TenantUsageConfiguration, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant id is required")
	}

	// 1) Try cache first
	var usageConfig string
	if s.redisService != nil {
		key := s.redisService.GenerateKey(redis.USAGE_CONFIG, tenantID)
		val, err := s.redisService.GetValue(ctx, key)
		if err != nil && err != redis.ErrKeyNotExist {
			logger.Base().Error("Failed to get usage config from redis")
		} else {
			usageConfig = val
		}
	}

	if usageConfig != "" {
		oc := &event.TenantUsageConfiguration{}
		if err := json.Unmarshal([]byte(usageConfig), oc); err == nil {
			return oc, nil
		}
		logger.Base().Error("Failed to unmarshal cached usage config, will refetch")
	}

	// 2) Fallback to source
	resp, err := s.GetTenantCurrentUsage(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Note: not writing back to cache here to keep behavior simple/lightweight.
	return resp.Config, nil
}

// SupportsLanguageSwitch returns true if usage allows language switching.
func (s *UsageService) SupportsLanguageSwitch(ctx context.Context, tenantID string) bool {
	cfg, err := s.getTenantUsageConfig(ctx, tenantID)
	if err != nil {
		logger.Base().Error("SupportsLanguageSwitch: failed to get usage config")
		return false
	}
	if cfg == nil {
		return false
	}
	// Prefer explicit voice_agent_config if present.
	if cfg.VoiceAgentConfig != nil {
		return cfg.VoiceAgentConfig.LanguageSwitch
	}
	return false
}

// SupportsAccentAdaptation returns true if usage allows accent adaptation.
func (s *UsageService) SupportsAccentAdaptation(ctx context.Context, tenantID string) bool {
	cfg, err := s.getTenantUsageConfig(ctx, tenantID)
	if err != nil {
		logger.Base().Error("SupportsAccentAdaptation: failed to get usage config")
		return false
	}
	if cfg == nil {
		return false
	}
	// Prefer explicit voice_agent_config if present.
	if cfg.VoiceAgentConfig != nil {
		return cfg.VoiceAgentConfig.AccentAdaptation
	}
	return false
}

func (s *UsageService) checkUsageLimits(config *event.TenantUsageConfiguration, category UsageCategory) (bool, string, error) {
	categoryName := s.getCategoryName(category)

	// Check specific usage configs for the requested category
	for _, usage := range config.UsageConfigs {
		if usage.Category == common.UsageCategory(category) {
			zap.L().Info("Found usage config for category",
				zap.String("category", categoryName),
				zap.Int64("used_amount", usage.UsedAmount),
				zap.Int64("total_amount", usage.TotalAmount),
				zap.Bool("unlimited", usage.TotalAmount == -1))

			if usage.TotalAmount == -1 {
				zap.L().Info("Usage check passed - unlimited quota",
					zap.String("category", categoryName))
				return true, "", nil // unlimited
			}
			if usage.UsedAmount >= usage.TotalAmount {
				zap.L().Warn("Usage check failed - quota exceeded",
					zap.String("category", categoryName),
					zap.Int64("used_amount", usage.UsedAmount),
					zap.Int64("total_amount", usage.TotalAmount))
				return false, fmt.Sprintf("%s usage limit reached. You have used %d out of %d available.", categoryName, usage.UsedAmount, usage.TotalAmount), nil
			}
			zap.L().Info("Usage check passed - quota available",
				zap.String("category", categoryName),
				zap.Int64("used_amount", usage.UsedAmount),
				zap.Int64("total_amount", usage.TotalAmount),
				zap.Int64("remaining", usage.TotalAmount-usage.UsedAmount))
			return true, "", nil
		}
	}

	// If no specific config found, return not allowed with appropriate message
	zap.L().Warn("Usage check failed - no config found for category",
		zap.String("category", categoryName),
		zap.Int("total_configs", len(config.UsageConfigs)))
	return false, fmt.Sprintf("%s feature is not available or configured for your current plan.", categoryName), nil
}

func (s *UsageService) getCategoryName(category UsageCategory) string {
	switch category {
	case UsageCategory_USAGE_CREDIT:
		return "Credit"
	case UsageCategory_USAGE_AI_AGENT:
		return "AI Agent"
	case UsageCategory_USAGE_INTEGRATION:
		return "Integration"
	case UsageCategory_USAGE_AGENT_KB:
		return "Agent Knowledge Base"
	default:
		return "Unknown"
	}
}
