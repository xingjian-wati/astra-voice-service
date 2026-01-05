package handler

import (
	"context"
	"net/http"
	"os"
	"time"

	httpadapter "github.com/ClareAI/astra-voice-service/internal/adapters/http"
	"github.com/ClareAI/astra-voice-service/internal/adapters/livekit"
	"github.com/ClareAI/astra-voice-service/internal/config"
	whatsappconfig "github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/model"
	"github.com/ClareAI/astra-voice-service/internal/core/model/openai"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/internal/core/session"
	"github.com/ClareAI/astra-voice-service/internal/core/task"
	"github.com/ClareAI/astra-voice-service/internal/repository"
	"github.com/ClareAI/astra-voice-service/internal/services/agent"
	"github.com/ClareAI/astra-voice-service/internal/services/call"
	"github.com/ClareAI/astra-voice-service/internal/storage"
	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/pkg/redis"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// HandlerManager manages all handlers and their initialization
type HandlerManager struct {
	config            *whatsappconfig.WhatsAppCallConfig
	service           *call.WhatsAppCallService
	watiClient        *httpadapter.WatiClient
	repoManager       repository.RepositoryManager
	coreOpenAIHandler *openai.Handler              // Core OpenAI handler for service layer
	coreTokenHandler  *openai.RealtimeTokenHandler // Core token handler
	composioService   *mcp.ComposioService         // Centralized MCP service
	taskBus           task.Bus                     // Task bus for asynchronous processing

	// Only store handlers that need to be accessed externally
	// Management handlers are used internally
	// LiveKit integration (NEW - optional, only initialized if enabled)
	livekitRoomManager *livekit.RoomManager
}

// NewHandlerManager creates and initializes all handlers and services
func NewHandlerManager(cfg *whatsappconfig.WhatsAppCallConfig) (*HandlerManager, error) {
	// Create WebSocket config for OpenAI client
	wsConfig := &config.WebSocketConfig{
		Port:          cfg.Port,
		OpenAIAPIKey:  cfg.OpenAIAPIKey,
		OpenAIBaseURL: cfg.OpenAIBaseURL,
	}

	// Create core OpenAI handler and token handler
	coreOpenAIHandler := openai.NewOpenAIHandler(wsConfig)
	coreTokenHandler := openai.NewRealtimeTokenHandler(wsConfig)

	// Pre-configure core handler with basic token generation
	// This ensures WhatsAppCallService has a working handler even before routes are fully set up
	coreOpenAIHandler.TokenGenerator = func(sessionType, model, voice, language string, speed float64, tools []interface{}) (string, error) {
		tokenReq := openai.EphemeralTokenRequest{
			Session: openai.SessionConfig{
				Type:  sessionType,
				Model: model,
				Audio: openai.AudioConfig{
					Output: openai.AudioOutputConfig{
						Voice: voice,
						Speed: speed,
					},
				},
				Tools:    tools,
				Language: language,
			},
		}
		return coreTokenHandler.GenerateTokenInternal(tokenReq)
	}

	// Initialize database connection
	repoManager, err := repository.NewRepositoryManager()
	if err != nil {
		logger.Base().Error("failed to connect to database", zap.Error(err))
		return nil, err
	}

	// Initialize Redis service for session management
	// We use the same redis config used in agent service
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}
	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379"
	}
	redisPassword := os.Getenv("REDIS_PASSWORD")

	redisConfig := &redis.RedisConfig{
		Host:     redisHost,
		Port:     redisPort,
		Password: redisPassword,
		DB:       0, // Default DB
	}
	redisSvc, err := redis.NewRedisService(redisConfig)
	if err != nil {
		logger.Base().Warn("failed to initialize redis service, running without session manager", zap.Error(err))
	}

	// Initialize Session Manager
	var sessionManager *session.Manager
	var taskBus *task.RedisBus
	if redisSvc != nil {
		podID := cfg.InstanceID
		if podID == "" {
			podID = "default-pod"
		}
		sessionManager = session.NewManager(redisSvc, podID)
		taskBus = task.NewRedisBus(redisSvc)
		logger.Base().Info("session manager and task bus initialized", zap.String("pod_id", podID))
	}

	// Create model factory
	modelFactory := model.NewProviderFactory()

	// Register our pre-configured core handler into the factory
	// This ensures that when the service layer asks the factory for an OpenAI handler,
	// it gets this configured instance instead of a raw unconfigured one.
	modelFactory.RegisterHandler(provider.ProviderTypeOpenAI, coreOpenAIHandler)

	// Initialize audio cache service if enabled
	logger.Base().Info("audio storage config",
		zap.Bool("enabled", cfg.AudioStorageEnabled),
		zap.String("type", cfg.AudioStorageType),
		zap.String("path", cfg.AudioStoragePath),
	)

	if cfg.AudioStorageEnabled && cfg.AudioStoragePath != "" {
		ctx := context.Background()
		storageType := storage.StorageType(cfg.AudioStorageType)
		if err := storage.InitAudioCache(ctx, true, storageType, cfg.AudioStoragePath); err != nil {
			logger.Base().Warn("failed to initialize audio cache, continue without caching",
				zap.Error(err),
				zap.String("type", cfg.AudioStorageType),
				zap.String("path", cfg.AudioStoragePath),
			)
		} else {
			logger.Base().Info("audio cache initialized",
				zap.String("type", cfg.AudioStorageType),
				zap.String("path", cfg.AudioStoragePath),
			)
		}
	} else {
		logger.Base().Info("audio cache disabled",
			zap.Bool("enabled", cfg.AudioStorageEnabled),
			zap.String("type", cfg.AudioStorageType),
			zap.String("path", cfg.AudioStoragePath),
		)
	}

	// Create Wati client with outbound base URL
	watiClient := httpadapter.NewWatiClient(cfg.WatiBaseURL, cfg.WatiTenantID, cfg.WatiAPIKey, cfg.OutboundBaseURL)

	// Create service with core OpenAI handler, factory, session manager, task bus and wati client
	service := call.NewWhatsAppCallService(cfg, coreOpenAIHandler, modelFactory, sessionManager, taskBus, watiClient)

	// Initialize ComposioService
	mcpConfig := config.LoadMCPServiceConfig()
	composioService := mcp.NewComposioService(mcpConfig.MCPServiceURL)

	// Get agent service for OpenAI handler configuration
	agentService, err := agent.GetAgentService()
	if err != nil {
		logger.Base().Warn("failed to get agent service", zap.Error(err))
	}

	// Configure core OpenAI handler using OpenAIHandler's configureOpenAIHandler method
	// Create a temporary OpenAIHandler to use its configureOpenAIHandler method
	tempOpenAIHandler := &OpenAIHandler{
		openaiHandler: coreOpenAIHandler,
		tokenHandler:  coreTokenHandler,
		agentService:  agentService,
	}
	tempOpenAIHandler.configureOpenAIHandler(service, composioService)

	logger.Base().Info("core openai handler configured")

	// Start distributed task processor after all handlers are configured
	if err := service.StartTaskProcessor(context.Background()); err != nil {
		logger.Base().Error("failed to start distributed task processor", zap.Error(err))
	}

	// Note: repoManager already initialized above

	// Initialize LiveKit room manager (NEW - only if enabled)
	var livekitRoomManager *livekit.RoomManager
	if cfg.LiveKitEnabled && cfg.LiveKitServerURL != "" {
		logger.Base().Info("initializing livekit integration",
			zap.Bool("livekit_enabled", cfg.LiveKitEnabled),
			zap.String("livekit_url", cfg.LiveKitServerURL),
		)
		livekitConfig, err := livekit.NewLiveKitConfig(
			cfg.LiveKitServerURL,
			cfg.LiveKitAPIKey,
			cfg.LiveKitAPISecret,
			cfg.LiveKitGCSBucket,
		)
		if err != nil {
			logger.Base().Warn("failed to create livekit config, disabled",
				zap.Error(err),
			)
		} else {
			livekitRoomManager, err = livekit.NewRoomManager(livekitConfig, service, coreOpenAIHandler)
			if err != nil {
				logger.Base().Warn("failed to initialize livekit room manager, disabled",
					zap.Error(err),
				)
			} else {
				// Start cleanup routine for expired connections
				go livekitRoomManager.StartCleanupRoutine(context.Background())
				logger.Base().Info("livekit integration initialized")
			}
		}
	} else {
		logger.Base().Info("livekit integration disabled",
			zap.Bool("livekit_enabled", cfg.LiveKitEnabled),
			zap.String("livekit_url", cfg.LiveKitServerURL),
		)
	}

	// Start automatic cleanup routine for inactive connections
	// This monitors conversation activity and cleans up connections that have been
	// inactive (no new messages) for more than the specified timeout
	go service.StartCleanupRoutine(
		context.Background(),
		2*time.Minute, // Check every 2 minutes
		5*time.Minute, // Cleanup connections inactive for 5+ minutes
	)

	return &HandlerManager{
		config:             cfg,
		service:            service,
		watiClient:         watiClient,
		repoManager:        repoManager,
		coreOpenAIHandler:  coreOpenAIHandler,
		coreTokenHandler:   coreTokenHandler,
		composioService:    composioService,
		taskBus:            taskBus,
		livekitRoomManager: livekitRoomManager,
	}, nil
}

// SetupAllRoutes sets up all routes with middleware
func (hm *HandlerManager) SetupAllRoutes(router *mux.Router) {
	// Apply global middleware
	router.Use(CORSMiddleware)
	router.Use(GlobalLoggingMiddleware)

	// Setup CRUD API routes
	hm.SetupAPIRoutes(router)

	// Setup static file routes
	hm.SetupStaticRoutes(router)

	// Setup WebRTC config routes
	hm.SetupWebRTCConfigRoutes(router)

	// Setup OpenAI routes
	hm.SetupOpenAIRoutes(router)

	// Setup Wati webhook routes
	hm.SetupWatiRoutes(router)

	// Setup Outbound webhook routes
	hm.SetupOutboundWebhookRoutes(router)

	// Setup LiveKit routes (NEW - only if enabled)
	if hm.livekitRoomManager != nil {
		hm.SetupLiveKitRoutes(router)
	}

	logger.Base().Info("all application routes registered")
}

// SetupAPIRoutes sets up all CRUD API routes and middleware
func (hm *HandlerManager) SetupAPIRoutes(router *mux.Router) {
	// Create API subrouter with middleware
	apiRouter := router.PathPrefix("/api").Subrouter()

	// Apply middleware to all API routes
	apiRouter.Use(LoggingMiddleware)
	apiRouter.Use(ValidationMiddleware)
	// Note: API key middleware is NOT applied here - API calls should work without authentication
	// API key is only used for frontend page access

	// Create handlers and setup routes (not stored in struct)
	agentHandler := NewAgentHandler(hm.repoManager, hm.composioService)
	agentHandler.SetupAgentRoutes(apiRouter)

	tenantHandler := NewTenantHandler(hm.repoManager.VoiceTenant())
	tenantHandler.SetupTenantRoutes(apiRouter)

	voiceConversationHandler := NewVoiceConversationHandler(hm.repoManager.VoiceConversation(), hm.repoManager.VoiceMessage())
	voiceConversationHandler.SetupVoiceConversationRoutes(apiRouter)

	// Setup CORS middleware for all API routes
	router.PathPrefix("/api/").HandlerFunc(handleCORS).Methods("OPTIONS")

	logger.Base().Info("crud api routes registered (no api key required)")
}

// SetupWebRTCConfigRoutes sets up WebRTC configuration routes
func (hm *HandlerManager) SetupWebRTCConfigRoutes(router *mux.Router) {
	webrtcConfigHandler := NewWebRTCConfigHandler(hm.service)
	webrtcConfigHandler.SetupWebRTCConfigRoutes(router)
}

// SetupStaticRoutes sets up static file routes
func (hm *HandlerManager) SetupStaticRoutes(router *mux.Router) {
	staticHandler := NewStaticHandler("static")

	// Setup static assets first (CSS, JS, images, fonts - no authentication needed)
	staticHandler.SetupStaticAssetsOnly(router)

	// Apply API key middleware to frontend pages only (not to static assets or API calls)
	if secretKey := os.Getenv("SECRET_KEY"); secretKey != "" {
		// Register frontend HTML page routes with API key middleware
		router.HandleFunc("/", APIKeyMiddleware(secretKey)(http.HandlerFunc(staticHandler.serveManagementDashboard)).ServeHTTP).Methods("GET")
		router.HandleFunc("/dashboard", APIKeyMiddleware(secretKey)(http.HandlerFunc(staticHandler.serveManagementDashboard)).ServeHTTP).Methods("GET")
		router.HandleFunc("/tenants", APIKeyMiddleware(secretKey)(http.HandlerFunc(staticHandler.serveTenantManagement)).ServeHTTP).Methods("GET")
		router.HandleFunc("/agents", APIKeyMiddleware(secretKey)(http.HandlerFunc(staticHandler.serveAgentManagement)).ServeHTTP).Methods("GET")
		router.HandleFunc("/test-client", APIKeyMiddleware(secretKey)(http.HandlerFunc(staticHandler.serveTestClient)).ServeHTTP).Methods("GET")
		router.HandleFunc("/test-webrtc", APIKeyMiddleware(secretKey)(http.HandlerFunc(staticHandler.serveTestWebRTC)).ServeHTTP).Methods("GET")
		router.HandleFunc("/test-livekit", APIKeyMiddleware(secretKey)(http.HandlerFunc(staticHandler.serveTestLiveKit)).ServeHTTP).Methods("GET")
		logger.Base().Info("frontend pages protected with api key middleware")
	} else {
		// If no SECRET_KEY, register pages without middleware (for development)
		router.HandleFunc("/", staticHandler.serveManagementDashboard).Methods("GET")
		router.HandleFunc("/dashboard", staticHandler.serveManagementDashboard).Methods("GET")
		router.HandleFunc("/tenants", staticHandler.serveTenantManagement).Methods("GET")
		router.HandleFunc("/agents", staticHandler.serveAgentManagement).Methods("GET")
		router.HandleFunc("/test-client", staticHandler.serveTestClient).Methods("GET")
		router.HandleFunc("/test-webrtc", staticHandler.serveTestWebRTC).Methods("GET")
		router.HandleFunc("/test-livekit", staticHandler.serveTestLiveKit).Methods("GET")
		logger.Base().Info("frontend pages registered without api key (development mode)")
	}

	logger.Base().Info("static file routes registered")
}

// SetupOpenAIRoutes sets up OpenAI-related routes
func (hm *HandlerManager) SetupOpenAIRoutes(router *mux.Router) {
	agentService, err := agent.GetAgentService()
	if err != nil {
		logger.Base().Warn("failed to get agent service for openai handler", zap.Error(err))
	}
	openaiHandler := NewOpenAIHandler(hm.config, hm.service, agentService, hm.composioService, hm.coreOpenAIHandler, hm.coreTokenHandler)
	openaiHandler.SetupOpenAIRoutes(router)
	logger.Base().Info("openai routes registered")
}

// SetupWatiRoutes sets up Wati webhook routes
func (hm *HandlerManager) SetupWatiRoutes(router *mux.Router) {
	agentID := getAgentID()
	watiHandler := NewWatiWebhookHandler(hm.service, hm.watiClient, hm.config.WatiWebhookSecret, agentID, hm.repoManager, hm.taskBus)
	watiHandler.SetupWatiRoutes(router)

	// Setup CORS preflight handling for all Wati routes
	router.PathPrefix("/wati/").HandlerFunc(handleCORS).Methods("OPTIONS")

	logger.Base().Info("wati webhook routes registered with CORS")
}

// SetupOutboundWebhookRoutes sets up outbound call webhook routes
func (hm *HandlerManager) SetupOutboundWebhookRoutes(router *mux.Router) {
	outboundWebhookHandler := NewOutboundWebhookHandler(hm.service, hm.watiClient, hm.repoManager, hm.taskBus)
	outboundWebhookHandler.SetupOutboundWebhookRoutes(router)

	logger.Base().Info("outbound webhook routes registered")
}

// SetupLiveKitRoutes sets up LiveKit routes (NEW)
func (hm *HandlerManager) SetupLiveKitRoutes(router *mux.Router) {
	livekitHandler := NewLiveKitHandler(hm.livekitRoomManager, hm.service, hm.taskBus)
	livekitHandler.SetupLiveKitRoutes(router)

	// Setup CORS preflight handling for all LiveKit routes
	router.PathPrefix("/livekit/").HandlerFunc(handleCORS).Methods("OPTIONS")

	logger.Base().Info("livekit routes registered with CORS")
}

// GetRepoManager returns the repository manager
func (hm *HandlerManager) GetRepoManager() repository.RepositoryManager {
	return hm.repoManager
}

// GetService returns the WhatsApp call service
func (hm *HandlerManager) GetService() *call.WhatsAppCallService {
	return hm.service
}

// GetWatiClient returns the Wati client
func (hm *HandlerManager) GetWatiClient() *httpadapter.WatiClient {
	return hm.watiClient
}

// GetCoreOpenAIHandler returns the core OpenAI handler
func (hm *HandlerManager) GetCoreOpenAIHandler() *openai.Handler {
	return hm.coreOpenAIHandler
}

// getAgentID determines the agent ID from environment variables
func getAgentID() string {
	agentID := whatsappconfig.GetDefaultAgentID()
	if envAgentID := os.Getenv("AGENT_ID"); envAgentID != "" {
		agentID = envAgentID
	} else if envBusinessType := os.Getenv("BUSINESS_TYPE"); envBusinessType != "" {
		// Support legacy BUSINESS_TYPE for backward compatibility
		agentID = whatsappconfig.GetAgentByBusinessType(envBusinessType)
	}
	return agentID
}

func SetupStaticRoutes(router *mux.Router, staticHandler *StaticHandler) {
	staticHandler.SetupStaticRoutes(router)
	logger.Base().Info("static file routes registered with handler")
}

// handleCORS handles CORS preflight requests for API routes
func handleCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusOK)
}
