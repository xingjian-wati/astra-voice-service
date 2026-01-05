package call

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	httpadapter "github.com/ClareAI/astra-voice-service/internal/adapters/http"
	webrtcadapter "github.com/ClareAI/astra-voice-service/internal/adapters/webrtc"
	apiconfig "github.com/ClareAI/astra-voice-service/internal/config"
	whatsappconfig "github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/event"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/internal/core/session"
	"github.com/ClareAI/astra-voice-service/internal/core/task"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/internal/services/agent"
	"github.com/ClareAI/astra-voice-service/pkg/data/api"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/pkg/pubsub"
	"github.com/ClareAI/astra-voice-service/pkg/twilio"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// WhatsAppCallService manages WhatsApp Call connections
type WhatsAppCallService struct {
	config          *whatsappconfig.WhatsAppCallConfig
	modelHandler    provider.ModelHandler    // Default handler
	modelFactory    provider.ProviderFactory // To support dynamic routing
	connections     map[string]*WhatsAppCallConnection
	mutex           sync.RWMutex
	webrtcProcessor *webrtcadapter.Processor

	// Twilio token service for dynamic TURN credentials
	twilioTokenService *twilio.TwilioTokenService

	// Event bus for minimal coordination
	eventBus event.EventBus

	// PubSub service for usage event publishing
	pubsubService *pubsub.PubSubService

	// Leads service for creating contacts
	leadsService *api.LeadsService

	// Session management for multi-pod coordination and monitoring
	sessionManager *session.Manager
	taskBus        task.Bus
	watiClient     *httpadapter.WatiClient
}

// NewWhatsAppCallService creates a new WhatsApp Call service
func NewWhatsAppCallService(config *whatsappconfig.WhatsAppCallConfig, defaultHandler provider.ModelHandler, factory provider.ProviderFactory, sessionManager *session.Manager, taskBus task.Bus, watiClient *httpadapter.WatiClient) *WhatsAppCallService {
	// Create minimal event bus
	eventBus := event.NewEventBus()

	service := &WhatsAppCallService{
		config:         config,
		modelHandler:   defaultHandler,
		modelFactory:   factory,
		connections:    make(map[string]*WhatsAppCallConnection),
		eventBus:       eventBus,
		sessionManager: sessionManager,
		taskBus:        taskBus,
		watiClient:     watiClient,
	}

	// Initialize session broadcast subscriber if manager is available
	if sessionManager != nil {
		logger.Base().Info("Subscribing to session cleanup broadcasts")
		sessionManager.SubscribeToCleanup(context.Background(), func(sessionID string) {
			// Check if we have this connection locally
			if conn := service.GetConnection(sessionID); conn != nil {
				logger.Base().Info("Received cleanup broadcast for local session, cleaning up...", zap.String("session_id", sessionID))
				service.CleanupConnection(sessionID)
			}
		})
	}

	// Initialize task processor if bus is available
	// Note: Subscription is now handled explicitly via Start() method
	// to ensure all handlers are fully configured before processing tasks.

	// Set default OnConnectionClose callback
	if defaultHandler != nil {
		defaultHandler.SetOnConnectionClose(func(connID string) {
			logger.Base().Info("Default model logic triggered connection close", zap.String("connection_id", connID))
			service.CleanupConnection(connID)
		})
	}

	// Initialize PubSub service for usage event publishing
	projectID := os.Getenv("PUBSUB_PROJECT_ID")
	topicName := os.Getenv("PUBSUB_TOPIC_NAME")
	pubID := os.Getenv("PUBSUB_PUB_ID")
	convMetricsPrefix := os.Getenv("PUBSUB_CONV_METRICS_PREFIX")
	if convMetricsPrefix == "" {
		convMetricsPrefix = "conversation:metrics:"
	}

	if projectID != "" && topicName != "" && pubID != "" {
		pubsubConfig := &pubsub.PubSubConfig{
			ProjectID:         projectID,
			TopicName:         topicName,
			PubID:             pubID,
			ConvMetricsPrefix: convMetricsPrefix,
		}
		pubsubService, err := pubsub.NewPubSubService(context.Background(), pubsubConfig)
		if err != nil {
			logger.Base().Error("Failed to initialize PubSub service")
		} else {
			service.pubsubService = pubsubService
			logger.Base().Info("PubSub service initialized for usage events")
		}
	} else {
		logger.Base().Info("PubSub not configured (requires PUBSUB_PROJECT_ID, PUBSUB_TOPIC_NAME, PUBSUB_PUB_ID)")
	}

	// Initialize Twilio token service if credentials are provided
	if config.TwilioAccountSID != "" && config.TwilioAuthToken != "" {
		service.twilioTokenService = twilio.NewTwilioTokenService(
			config.TwilioAccountSID,
			config.TwilioAuthToken,
			true, // Enable auto-refresh
		)
		logger.Base().Info("Twilio Network Traversal Service initialized")
	} else {
		logger.Base().Info("Twilio NTS not configured, using static TURN servers if provided")
	}

	// Initialize Leads service for contact creation
	apiServiceConfig := apiconfig.LoadAPIServiceConfig()
	if apiServiceConfig.APIServiceURL != "" {
		service.leadsService = api.NewLeadsService(apiServiceConfig.APIServiceURL)
		logger.Base().Info("Leads service initialized", zap.String("api_service_url", apiServiceConfig.APIServiceURL))
	} else {
		logger.Base().Info("Leads service not configured (API service URL not found)")
	}

	// Initialize WebRTC processor
	service.webrtcProcessor = webrtcadapter.NewProcessor(service)

	return service
}

// SetupRoutes sets up HTTP routes for WhatsApp Call webhooks
func (s *WhatsAppCallService) SetupRoutes(router *mux.Router) {
	// Legacy endpoints - kept for compatibility but not used in Wati-only mode
	router.HandleFunc("/whatsapp/status", s.handleStatus).Methods("GET")
	router.HandleFunc("/whatsapp/health", s.handleHealth).Methods("GET")
}

// initializeAIConnection establishes a model connection via WebRTC
func (s *WhatsAppCallService) initializeAIConnection(connection *WhatsAppCallConnection) {
	s.initializeAIConnectionWithSignalControl(connection, connection.IsOutboundCall)
}

// initializeAIConnectionWithSignalControl establishes a model connection via WebRTC
// with optional greeting signal control for delayed greeting
func (s *WhatsAppCallService) initializeAIConnectionWithSignalControl(connection *WhatsAppCallConnection, enableSignalControl bool) {
	logger.Base().Info("Initializing model connection", zap.String("connection_id", connection.ID), zap.String("voice_language", connection.VoiceLanguage), zap.Bool("is_outbound_call", connection.IsOutboundCall))

	// Determine provider type from connection info (if available)
	providerType := provider.ProviderTypeOpenAI
	if connection.ModelProvider == provider.ProviderTypeGemini {
		providerType = provider.ProviderTypeGemini
	}

	modelHandler, err := s.GetModelHandler(providerType)
	if err != nil {
		logger.Base().Error("Failed to get model handler", zap.Error(err))
		s.cleanupConnection(connection.ID)
		return
	}

	// Enable signal control if requested (typically for outbound calls)
	if enableSignalControl {
		modelHandler.EnableGreetingSignalControl(connection.ID)
	}

	// Initialize model connection
	modelConn, err := modelHandler.InitializeConnectionWithLanguage(connection.ID, connection.VoiceLanguage, connection.Accent)
	if err != nil {
		logger.Base().Error("Failed to establish model connection", zap.String("connection_id", connection.ID), zap.Error(err))
		s.cleanupConnection(connection.ID)
		return
	}

	// Store model connection and handler for this specific call
	connection.ModelConnection = modelConn
	connection.ModelHandler = modelHandler

	// For backward compatibility
	type clientGetter interface {
		GetClient() *webrtcadapter.Client
	}
	if cg, ok := modelConn.(clientGetter); ok {
		connection.AIWebRTC = cg.GetClient()
	}

	connection.IsAIReady = true
	connection.LastActivity = time.Now()

	logger.Base().Info("Model WebRTC connection established", zap.String("connection_id", connection.ID), zap.String("provider", string(providerType)))
	s.eventBus.Publish(event.AIConnectionInit, &event.AIEventData{
		ConnectionID: connection.ID,
	})
}

// Legacy handlers - kept for compatibility
func (s *WhatsAppCallService) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok", "service": "whatsapp-call-gateway"}`))
}

func (s *WhatsAppCallService) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"health": "ok"}`))
}

// StartTaskProcessor begins subscribing to the task bus for distributed call processing
func (s *WhatsAppCallService) StartTaskProcessor(ctx context.Context) error {
	if s.taskBus != nil {
		logger.Base().Info("Starting distributed task processor subscription")
		return s.taskBus.Subscribe(ctx, s.handleSessionTask)
	}
	return nil
}

// Connection management methods
func (s *WhatsAppCallService) AddConnection(connection *WhatsAppCallConnection) {
	s.mutex.Lock()
	s.connections[connection.ID] = connection
	s.mutex.Unlock()

	// Register session for monitoring if manager is available
	if s.sessionManager != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s.sessionManager.Register(ctx, session.SessionInfo{
				SessionID:   connection.ID,
				AgentID:     connection.AgentID,
				StartTime:   connection.CreatedAt,
				ChannelType: string(connection.ChannelType),
			})
		}()
	}
}

func (s *WhatsAppCallService) GetConnection(connectionID string) webrtcadapter.ConnectionInterface {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	conn, ok := s.connections[connectionID]
	if !ok || conn == nil {
		return nil
	}
	return conn
}

// GetAllConnections returns all active connections (for internal use)
func (s *WhatsAppCallService) GetAllConnections() map[string]*WhatsAppCallConnection {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Return a copy to avoid external modification
	connections := make(map[string]*WhatsAppCallConnection, len(s.connections))
	for id, conn := range s.connections {
		connections[id] = conn
	}
	return connections
}

// GetConnectionByPhone finds an active connection by phone number
func (s *WhatsAppCallService) GetConnectionByPhone(phoneNumber string) *WhatsAppCallConnection {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Search through all active connections for matching phone number
	for _, connection := range s.connections {
		if connection.From == phoneNumber && connection.IsActive {
			return connection
		}
	}
	return nil
}

func (s *WhatsAppCallService) GetConnectionCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.connections)
}

func (s *WhatsAppCallService) cleanupConnection(connectionID string) {
	// Remove connection from map first and get its data
	s.mutex.Lock()
	connection, exists := s.connections[connectionID]
	if !exists {
		s.mutex.Unlock()
		return
	}
	// Remove from map immediately to prevent duplicate cleanup
	delete(s.connections, connectionID)
	s.mutex.Unlock()

	// Calculate call duration
	callDuration := time.Since(connection.CreatedAt)
	durationSeconds := int32(callDuration.Seconds())

	logger.Base().Info("🧹 Cleaning up connection", zap.String("connection_id", connectionID), zap.Duration("duration", callDuration))

	// Unregister session from monitoring if manager is available
	if s.sessionManager != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s.sessionManager.Unregister(ctx, connectionID)
		}()
	}

	// Mark connection as inactive (connection already removed from map, so no lock needed for IsActive)
	// Direct field access is safe here since connection is removed from map and won't be accessed by other paths
	connection.IsActive = false
	atomic.StoreInt32(&connection.AtomicClosed, 1) // Atomic closed state for audio loops

	// Read connection fields (safe to read without lock since connection is removed from map
	// and these fields are not modified during cleanup)
	agentID := connection.AgentID
	conversationID := connection.ConversationID
	channelType := connection.ChannelType
	wasConnected := conversationID != "" || len(connection.ConversationHistory) > 0

	// Cache language info before model handler cleanup clears in-memory state
	cachedLanguages := make([]string, 0, 2)
	langSet := make(map[string]struct{}, 2)
	addLang := func(lang string) {
		if lang == "" {
			return
		}
		if _, ok := langSet[lang]; ok {
			return
		}
		langSet[lang] = struct{}{}
		cachedLanguages = append(cachedLanguages, lang)
	}
	addLang(connection.VoiceLanguage)
	if connection.ModelHandler != nil {
		if lang, _ := connection.ModelHandler.GetCurrentLanguageAccent(connection.ID); lang != "" {
			addLang(lang)
		}
	}

	// Get agent's tenantID (not connection's tenantID) for billing purposes
	tenantID := s.getTenantIDForBilling(connection, agentID)

	// Get TextAgentID for contact creation
	var textAgentID string
	if agentService, err := agent.GetAgentService(); err == nil {
		if agentConfig, err := agentService.GetAgentConfig(context.Background(), agentID); err == nil {
			textAgentID = agentConfig.TextAgentID
		} else {
			logger.Base().Error("Failed to get agent config", zap.String("agent_id", agentID), zap.Error(err))
		}
	}

	// Fallback to agentID if textAgentID is empty
	if textAgentID == "" {
		textAgentID = agentID
	}

	// Mark conversation as ended in database
	s.endConversationInDB(connection)

	// Close model connection
	logger.Base().Debug("Checking model connection", zap.Bool("has_webrtc_client", connection.AIWebRTC != nil), zap.Bool("is_model_ready", connection.IsAIReady))
	if connection.ModelConnection != nil {
		logger.Base().Info("🔌 Closing model connection")
		if connection.ModelHandler != nil {
			connection.ModelHandler.CloseConnection(connection.ID)
		}
	} else if connection.IsAIReady {
		// Fallback: Model might be ready but ModelConnection field not set
		logger.Base().Info("🔌 Closing model connection via IsAIReady fallback")
		if connection.ModelHandler != nil {
			connection.ModelHandler.CloseConnection(connection.ID)
		}
	} else {
		logger.Base().Warn("Model connection not found or not ready, skipping close")
	}

	// Close WebRTC processor connection (PeerConnection)
	if s.webrtcProcessor != nil {
		s.webrtcProcessor.CleanupConnection(connectionID)
	}

	// Clean up Opus decoder for this connection
	// Note: We do NOT set OpusDecoder to nil here anymore.
	// The decoder is used in the audio forwarding loop (goroutine), and setting it to nil
	// while that loop is running can cause race conditions or crashes if the loop
	// has already checked for nil but hasn't finished decoding.
	// Instead, we let Go's GC handle the cleanup when the connection object is no longer referenced.
	// Since the audio loop holds a reference to the connection, the decoder will be kept alive
	// until the loop exits (which happens when PeerConnection is closed).
	/*
		if connection.OpusDecoder != nil {
			connection.Mutex.Lock()
			connection.OpusDecoder = nil
			connection.Mutex.Unlock()
			logger.Base().Info("🧹 Cleaned up Opus decoder for connection", zap.String("connection_id", connectionID))
		}
	*/

	// Publish voice message usage event
	if s.pubsubService != nil && tenantID != "" && agentID != "" {
		// Skip usage reporting for test mode calls or default tenants
		if connection.ChannelType == domain.ChannelTypeTest ||
			connection.ChannelType == domain.ChannelTypeLiveKit ||
			tenantID == whatsappconfig.DefaultTenantID ||
			tenantID == whatsappconfig.DefaultWatiTenantID {
			logger.Base().Info("Skipping voice usage event for test/default tenant",
				zap.String("connection_id", connectionID),
				zap.String("tenant_id", tenantID),
				zap.String("channel", string(connection.ChannelType)))
		} else {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if !connection.WasConnected() {
					logger.Base().Warn("Connection was not connected, skipping voice usage event", zap.String("connection_id", connectionID))
					return
				}

				if err := s.pubsubService.PublishVoiceMessageUsageEvent(ctx, tenantID, agentID, durationSeconds); err != nil {
					logger.Base().Error("Failed to publish voice usage event", zap.String("connection_id", connectionID), zap.Error(err))
				} else {
					logger.Base().Info("Published voice usage event", zap.String("agent_id", agentID), zap.String("tenant_id", tenantID), zap.Int32("duration_seconds", durationSeconds))
				}
			}()
		}
	}

	// Publish conversation metrics event (voice agent)
	if s.pubsubService != nil && tenantID != "" && agentID != "" {
		// Skip metrics reporting for test mode calls or default tenants
		if connection.ChannelType == domain.ChannelTypeTest ||
			connection.ChannelType == domain.ChannelTypeLiveKit ||
			tenantID == whatsappconfig.DefaultTenantID ||
			tenantID == whatsappconfig.DefaultWatiTenantID {
			logger.Base().Info("Skipping conversation metrics event for test/default tenant",
				zap.String("connection_id", connectionID),
				zap.String("tenant_id", tenantID))
		} else if !wasConnected {
			logger.Base().Warn("Connection was not connected, skipping conversation metrics event", zap.String("connection_id", connectionID))
		} else {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				// Derive end time and message timeline
				endAt := time.Now()
				messages := make([]pubsub.Message, 0, len(connection.ConversationHistory))
				languages := append([]string{}, cachedLanguages...)

				// Build messages from in-memory history (IDs pre-cached on append)
				for _, msg := range connection.ConversationHistory {
					if msg.Role != whatsappconfig.MessageRoleUser && msg.Role != whatsappconfig.MessageRoleAssistant {
						continue
					}
					ts := msg.Timestamp
					formatted := ts.Format(time.RFC3339)
					msgID := msg.ID
					if msgID == "" {
						msgID = fmt.Sprintf("%s-%d", connectionID, len(messages)+1)
					}
					messages = append(messages, pubsub.Message{
						ID:      msgID,
						StartAt: formatted,
						EndAt:   formatted,
					})
				}

				metrics := pubsub.ConversationMetricsEvent{
					ID:        conversationID,
					UserID:    connection.From,
					TenantID:  tenantID,
					AgentID:   textAgentID, // use text agent id for metrics
					Channel:   "voice",
					Language:  strings.Join(uniqueNonEmpty(languages), ","),
					Status:    domain.CallStatusEnded,
					StartAt:   connection.CreatedAt,
					EndAt:     &endAt,
					Duration:  int(durationSeconds),
					TurnCount: len(messages),
					Messages:  messages,
					Actions:   connection.Actions,
					CreatedAt: endAt,
				}

				if metrics.ID == "" {
					metrics.ID = connectionID
				}

				if err := s.pubsubService.PublishConversationMetricsEvent(ctx, metrics); err != nil {
					logger.Base().Error("Failed to publish conversation metrics event for connection", zap.String("connection_id", connectionID), zap.Error(err))
				} else {
					logger.Base().Info("📊 Published conversation metrics event", zap.String("id", metrics.ID), zap.String("tenant_id", tenantID), zap.String("agent_id", textAgentID), zap.Int32("duration_seconds", durationSeconds))
				}
			}()
		}
	}

	// Create contact lead if service is available and connection was connected
	// Skip for default tenants as they won't have agent info in the leads system
	if s.leadsService != nil && wasConnected &&
		tenantID != whatsappconfig.DefaultTenantID &&
		tenantID != whatsappconfig.DefaultWatiTenantID {
		// Validate required fields
		if agentID == "" || tenantID == "" {
			logger.Base().Error("Missing required fields for contact creation", zap.String("agent_id", agentID), zap.String("tenant_id", tenantID))
		} else {
			// Determine mode (default to production)
			mode := "production"
			if channelType == domain.ChannelTypeTest {
				mode = "test"
			}

			// Create request with reused values
			req := api.CreateContactRequest{
				AgentID:         textAgentID, // Use textAgentID
				ConversationID:  conversationID,
				Mode:            mode,
				InteractionType: "voice",  // Constant
				TenantID:        tenantID, // Reused from line 238
				PhoneNumber:     connection.From,
			}

			// Call asynchronously to avoid blocking cleanup
			go func() {
				resp, err := s.leadsService.CreateContact(req)
				if err != nil {
					logger.Base().Error("Failed to create contact lead for conversation", zap.String("conversation_id", conversationID), zap.Error(err))
				} else {
					logger.Base().Info("Contact lead creation initiated for conversation", zap.String("conversation_id", conversationID), zap.String("agent_id", agentID), zap.String("tenant_id", tenantID))
					// Sync voice chat
					syncReq := api.SyncVoiceChatRequest{
						AgentID:        textAgentID,
						ConversationID: conversationID,
						ContactID:      resp.ContactID,
					}
					if _, err := s.leadsService.SyncVoiceChat(syncReq); err != nil {
						logger.Base().Error("Failed to sync voice chat for conversation", zap.String("conversation_id", conversationID), zap.Error(err))
					} else {
						logger.Base().Info("Voice chat synced for conversation", zap.String("conversation_id", conversationID))
					}
				}
			}()
		}
	}

	logger.Base().Info("Connection cleanup completed", zap.String("connection_id", connectionID))

	// Publish termination event to local bus
	s.eventBus.Publish(event.ConnectionTerminated, event.NewConnectionEvent(event.ConnectionTerminated, connectionID))
}

// endConversationInDB handles the database update for ending a conversation
func (s *WhatsAppCallService) endConversationInDB(connection *WhatsAppCallConnection) {
	if connection.RepoManager != nil {
		convID := connection.GetConversationID()
		if convID != "" {
			// Use a short timeout for DB operations
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			repo := connection.RepoManager.VoiceConversation()
			conv, err := repo.GetByID(ctx, convID)
			if err == nil && conv != nil {
				if err := repo.EndConversation(ctx, conv); err != nil {
					logger.Base().Error("Failed to end voice conversation in DB", zap.String("conversation_id", convID), zap.Error(err))
				} else {
					logger.Base().Info("Voice conversation ended in DB", zap.String("conversation_id", convID))
				}
			}
		}
	}
}

// getTenantIDForBilling gets the tenant ID for billing purposes (prefer agent's tenant over connection's tenant)
func (s *WhatsAppCallService) getTenantIDForBilling(connection *WhatsAppCallConnection, agentID string) string {
	// Default to connection's tenantID
	fallbackTenantID := connection.GetTenantID()

	// Return fallback if no agentID
	if agentID == "" {
		logger.Base().Warn("No agentID, using connection tenantID", zap.String("fallback_tenant_id", fallbackTenantID))
		return fallbackTenantID
	}

	// Try to get agent service
	agentService, err := agent.GetAgentService()
	if err != nil {
		logger.Base().Warn("AgentService unavailable, using connection tenantID", zap.String("fallback_tenant_id", fallbackTenantID), zap.Error(err))
		return fallbackTenantID
	}

	// Try to get agent's tenantID
	agentTenantID, err := agentService.GetTenantIDByAgentID(agentID)
	if err != nil {
		logger.Base().Error("Failed to get agent tenantID, using connection tenantID", zap.String("fallback_tenant_id", fallbackTenantID), zap.String("agent_id", agentID), zap.Error(err))
		return fallbackTenantID
	}

	// Success - use agent's tenantID
	logger.Base().Info("📊 Using agent's tenantID for billing", zap.String("agent_id", agentID), zap.String("agent_tenant_id", agentTenantID))
	return agentTenantID
}

// GetWebRTCProcessor returns the WebRTC processor
func (s *WhatsAppCallService) GetWebRTCProcessor() *webrtcadapter.Processor {
	return s.webrtcProcessor
}

// GetEventBus returns the event bus
func (s *WhatsAppCallService) GetEventBus() event.EventBus {
	return s.eventBus
}

// InitializeAIConnection initializes model connection for a connection (exported)
func (s *WhatsAppCallService) InitializeAIConnection(connection *WhatsAppCallConnection) {
	s.initializeAIConnection(connection)
}

// GetModelHandler returns the model handler for direct access to signal control
func (s *WhatsAppCallService) GetModelHandler(providerType provider.ProviderType) (provider.ModelHandler, error) {
	if providerType == "" {
		return s.modelHandler, nil
	}

	if s.modelFactory != nil {
		wsConfig := &apiconfig.WebSocketConfig{
			OpenAIAPIKey:  s.config.OpenAIAPIKey,
			OpenAIBaseURL: s.config.OpenAIBaseURL,
			GeminiAPIKey:  s.config.GeminiAPIKey,
			GeminiBaseURL: s.config.GeminiBaseURL,
			GeminiModel:   s.config.GeminiModel,
		}
		handler, err := s.modelFactory.CreateHandler(providerType, wsConfig)
		if err == nil {
			handler.SetOnConnectionClose(func(connID string) {
				s.CleanupConnection(connID)
			})
			return handler, nil
		}
	}
	return s.modelHandler, nil
}

// GetConnectionByCallID finds a connection by CallID
func (s *WhatsAppCallService) GetConnectionByCallID(callID string) (*WhatsAppCallConnection, string) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for id, conn := range s.connections {
		// Use connection-level lock to ensure thread-safe read
		conn.Mutex.RLock()
		matches := conn.CallID == callID
		conn.Mutex.RUnlock()

		if matches {
			return conn, id
		}
	}
	return nil, ""
}

// CleanupConnection cleans up a connection (exported wrapper)
func (s *WhatsAppCallService) CleanupConnection(connectionID string) {
	s.cleanupConnection(connectionID)
}

// CleanupExpiredConnections removes connections that have been inactive for a duration
func (s *WhatsAppCallService) CleanupExpiredConnections(inactivityDuration time.Duration) int {
	// Collect expired connection IDs while holding the lock
	s.mutex.Lock()
	now := time.Now()
	var expiredIDs []string

	for id, conn := range s.connections {
		// Check if connection is inactive for too long
		inactiveDuration := now.Sub(conn.LastActivity)
		if inactiveDuration > inactivityDuration {
			expiredIDs = append(expiredIDs, id)
			logger.Base().Info("🗑 Connection inactive for too long", zap.String("connection_id", id), zap.Duration("inactive_duration", inactiveDuration))
		}
	}
	s.mutex.Unlock()

	// Cleanup connections after releasing the lock to avoid deadlock
	cleanedCount := 0
	for _, id := range expiredIDs {
		s.cleanupConnection(id)
		cleanedCount++
	}

	if cleanedCount > 0 {
		logger.Base().Info("Cleaned up expired connections", zap.Int("cleaned_count", cleanedCount))
	}

	return cleanedCount
}

// StartCleanupRoutine starts a background routine to clean up expired connections
func (s *WhatsAppCallService) StartCleanupRoutine(ctx context.Context, checkInterval, inactivityTimeout time.Duration) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	logger.Base().Info("Started cleanup routine", zap.Duration("check_interval", checkInterval), zap.Duration("inactivity_timeout", inactivityTimeout))
	for {
		select {
		case <-ctx.Done():
			logger.Base().Info("🛑 Routine stopped")
			return
		case <-ticker.C:
			cleanedCount := s.CleanupExpiredConnections(inactivityTimeout)
			if cleanedCount > 0 {
				logger.Base().Info("🧹 Periodic check: cleaned connections", zap.Int("cleaned_count", cleanedCount))
			}
		}
	}
}

// GetConversationIDByConnectionID gets the conversation ID for a given connection ID
func (s *WhatsAppCallService) GetConversationIDByConnectionID(connectionID string) (string, error) {
	connection := s.GetConnection(connectionID)
	if connection == nil {
		return "", fmt.Errorf("connection not found: %s", connectionID)
	}

	// GetConversationID handles locking internally, so we don't need to lock here
	conversationID := connection.GetConversationID()
	if conversationID == "" {
		return "", fmt.Errorf("conversation ID not available for connection: %s", connectionID)
	}

	return conversationID, nil
}

// GetConversationIDByCallID gets the conversation ID for a given call ID
func (s *WhatsAppCallService) GetConversationIDByCallID(callID string) (string, error) {
	connection, _ := s.GetConnectionByCallID(callID)
	if connection == nil {
		return "", fmt.Errorf("connection not found for call ID: %s", callID)
	}

	conversationID := connection.GetConversationID()
	if conversationID == "" {
		return "", fmt.Errorf("conversation ID not available for call ID: %s", callID)
	}

	return conversationID, nil
}

// CleanupConnectionByCallID cleans up a connection by CallID
func (s *WhatsAppCallService) CleanupConnectionByCallID(callID string) {
	// Find connection by call ID and remove from map
	s.mutex.Lock()
	var connectionID string
	var foundConnection *WhatsAppCallConnection

	for id, connection := range s.connections {
		if connection.CallID == callID {
			connectionID = id
			foundConnection = connection
			// Mark connection as inactive
			connection.IsActive = false
			// Remove from connections map immediately
			delete(s.connections, id)
			break
		}
	}
	s.mutex.Unlock()

	// Perform cleanup operations after releasing the lock to avoid deadlock
	if foundConnection != nil {
		logger.Base().Info("Cleaning up connection by CallID", zap.String("connection_id", connectionID), zap.String("call_id", callID))

		// Unregister session from monitoring if manager is available
		if s.sessionManager != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				s.sessionManager.Unregister(ctx, connectionID)
			}()
		}

		// Mark conversation as ended in database
		s.endConversationInDB(foundConnection)

		// Close model connection
		if foundConnection.ModelConnection != nil {
			s.modelHandler.CloseConnection(connectionID)
		}

		// Close WebRTC processor connection
		if s.webrtcProcessor != nil {
			s.webrtcProcessor.CleanupConnection(connectionID)
		}

		logger.Base().Info("Connection cleanup completed", zap.String("connection_id", connectionID))
	} else {
		logger.Base().Warn("Connection not found for CallID", zap.String("call_id", callID))
	}
}

// NotifyCleanup broadcasts a cleanup request to all pods via session manager
func (s *WhatsAppCallService) NotifyCleanup(ctx context.Context, sessionID string) error {
	// Always cleanup locally first for immediate effect on the current pod
	s.CleanupConnection(sessionID)

	if s.sessionManager != nil {
		return s.sessionManager.NotifyCleanup(ctx, sessionID)
	}
	return nil
}

// NotifyCleanupByCallID broadcasts a cleanup request by CallID
func (s *WhatsAppCallService) NotifyCleanupByCallID(ctx context.Context, callID string) error {
	// Find connection by call ID
	s.mutex.RLock()
	var connectionID string
	for id, connection := range s.connections {
		if connection.CallID == callID {
			connectionID = id
			break
		}
	}
	s.mutex.RUnlock()

	if connectionID != "" {
		return s.NotifyCleanup(ctx, connectionID)
	}

	// If not found locally, we still broadcast because it might be on another pod
	if s.sessionManager != nil {
		logger.Base().Info("CallID not found locally, broadcasting cleanup by session ID (using CallID as proxy)", zap.String("call_id", callID))
		return s.sessionManager.NotifyCleanup(ctx, callID)
	}
	return nil
}

// GetSTUNServers returns the configured STUN servers
func (s *WhatsAppCallService) GetSTUNServers() []string {
	return s.config.STUNServers
}

// GetTURNCredentials returns TURN credentials from Twilio
func (s *WhatsAppCallService) GetTURNCredentials() []webrtcadapter.TURNCredentials {
	if s.twilioTokenService == nil || !s.twilioTokenService.IsEnabled() {
		return nil
	}
	pkgCreds := s.twilioTokenService.GetTURNCredentials()
	// Convert twilio.TURNCredentials to webrtcadapter.TURNCredentials
	webrtcCreds := make([]webrtcadapter.TURNCredentials, len(pkgCreds))
	for i, cred := range pkgCreds {
		webrtcCreds[i] = webrtcadapter.TURNCredentials{
			URLs:       cred.URLs,
			Username:   cred.Username,
			Credential: cred.Credential,
		}
	}
	return webrtcCreds
}

// uniqueNonEmpty returns deduplicated non-empty strings in order.
func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// handleSessionTask processes asynchronous session initialization tasks
func (s *WhatsAppCallService) handleSessionTask(t task.SessionTask) {
	logger.Base().Info("Processing session task", zap.String("type", string(t.Type)), zap.String("conn_id", t.ConnectionID))

	// Find the local connection
	conn := s.GetConnection(t.ConnectionID)
	if conn == nil {
		// Not on this pod, ignore
		return
	}

	connection, ok := conn.(*WhatsAppCallConnection)
	if !ok {
		return
	}

	switch t.Type {
	case task.TaskTypeInboundCall:
		// Recover payload (raw webhook body)
		var event httpadapter.WatiWebhookEvent
		if err := json.Unmarshal(t.Payload, &event); err != nil {
			logger.Base().Error("Failed to unmarshal inbound call event", zap.Error(err))
			return
		}

		// Handle SDP Offer synchronously on the owning Pod
		sdpData, err := event.ParseSDP()
		if err == nil && sdpData != nil && sdpData.Type == "offer" {
			// Pass the parsed SDP data to avoid re-parsing
			s.handleAsyncInboundCall(event.TenantID, event.CallID, sdpData.SDP, connection)
		} else {
			// No SDP, just init AI
			s.initializeAIConnection(connection)
		}

	case task.TaskTypeWebCall:
		// Web/Test calls don't call Wati API for accept, just start processing
		connection.HasInboundAudio = true
		s.initializeAIConnection(connection)

	case task.TaskTypeOutboundCall:
		// Process outbound call setup (triggered when user answers)
		connection.HasInboundAudio = true
		s.initializeAIConnection(connection)

	case task.TaskTypeLiveKitRoom:
		// Process LiveKit room setup
		s.initializeAIConnection(connection)
	}
}

// handleAsyncInboundCall contains logic extracted from handler to be run by worker
func (s *WhatsAppCallService) handleAsyncInboundCall(tenantID, callID, offerSDP string, connection *WhatsAppCallConnection) {
	// 1. Generate SDP Answer
	sdpAnswer, err := s.webrtcProcessor.ProcessSDPOffer(connection.ID, offerSDP)
	if err != nil {
		logger.Base().Error("Failed to generate SDP answer", zap.Error(err))
		return
	}

	connection.SDPAnswer = sdpAnswer
	connection.LocalSDP = sdpAnswer

	// 2. Accept call via Wati API
	if s.watiClient != nil {
		logger.Base().Info("Calling Wati API accept (asynchronous worker)...")
		if err := s.watiClient.AcceptCallWithTenant(tenantID, callID, sdpAnswer); err != nil {
			logger.Base().Error("Wati API accept failed in worker", zap.Error(err))
			return
		}
		logger.Base().Info("Wati API accept successful, starting AI processing")

		// 3. Initialize AI connection
		s.initializeAIConnection(connection)
	} else {
		logger.Base().Warn("WatiClient not available in worker, skipping accept call")
	}
}
