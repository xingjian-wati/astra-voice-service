package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	httpadapter "github.com/ClareAI/astra-voice-service/internal/adapters/http"
	"github.com/ClareAI/astra-voice-service/internal/config"
	localconfig "github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/internal/repository"
	"github.com/ClareAI/astra-voice-service/internal/services/agent"
	"github.com/ClareAI/astra-voice-service/pkg/data/mapping"
	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// AgentHandler handles HTTP requests for voice agents
type AgentHandler struct {
	repoMgr         repository.RepositoryManager
	agentService    *agent.AgentService
	difyRepoMgr     *repository.DifyRepositoryManager
	composioService *mcp.ComposioService
	mappingService  *mapping.MappingService
}

// NewAgentHandler creates a new agent handler
func NewAgentHandler(repoMgr repository.RepositoryManager, composioService *mcp.ComposioService) *AgentHandler {
	agentService, err := agent.GetAgentService()
	if err != nil {
		logger.Base().Warn("Failed to get agent service", zap.Error(err))
	}

	// Initialize Dify repository manager for api_tokens queries
	difyRepoMgr, err := repository.NewDifyRepositoryManagerFromEnv()
	if err != nil {
		logger.Base().Warn("Failed to initialize Dify repository manager", zap.Error(err))
		logger.Base().Warn("API token queries will not be available")
	}

	// Initialize Mapping service
	mappingServiceBaseURL := os.Getenv("MAPPING_SERVICE_BASE_URL")
	var mappingService *mapping.MappingService
	if mappingServiceBaseURL != "" {
		mappingService = mapping.NewMappingService(mappingServiceBaseURL)
		logger.Base().Info("Mapping service initialized", zap.String("mapping_service_base_url", mappingServiceBaseURL))
	} else {
		logger.Base().Warn("Mapping service not configured", zap.String("env_var", "MAPPING_SERVICE_BASE_URL"))
	}

	return &AgentHandler{
		repoMgr:         repoMgr,
		agentService:    agentService,
		difyRepoMgr:     difyRepoMgr,
		composioService: composioService,
		mappingService:  mappingService,
	}
}

// CreateAgent godoc
// @Summary Create a new voice agent
// @Description Create a new voice agent with the specified configuration
// @Tags agents
// @Accept json
// @Produce json
// @Param agent body domain.CreateVoiceAgentRequest true "Agent creation request"
// @Success 201 {object} domain.VoiceAgent "Agent created successfully"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents [post]
func (h *AgentHandler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateVoiceAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// ✅ 自动设置 RAG 配置（如果未提供）
	h.ensureRAGConfig(r.Context(), &req.AgentConfig, req.TextAgentID)

	agent, err := h.repoMgr.VoiceAgent().Create(r.Context(), &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync newly created agent to cache (incremental update)
	if h.agentService != nil {
		if syncErr := h.agentService.SyncAgentCreate(r.Context(), agent); syncErr != nil {
			logger.Base().Warn("failed to sync created agent to cache",
				zap.Error(syncErr),
				zap.String("agent_id", agent.ID),
				zap.String("tenant_id", agent.VoiceTenantID),
			)
		}
	}

	// Register tools with MCP (Draft)
	if err := h.registerToolsWithMCP(r.Context(), agent, localconfig.AgentConfigModeDraft); err != nil {
		logger.Base().Warn("failed to register tools with MCP",
			zap.Error(err),
			zap.String("agent_id", agent.ID),
			zap.String("tenant_id", agent.VoiceTenantID),
			zap.String("mode", localconfig.AgentConfigModeDraft),
		)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(agent)
}

// GetAgent godoc
// @Summary Get agent by ID
// @Description Retrieve a specific voice agent by its unique identifier
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID (UUID)" format(uuid)
// @Success 200 {object} domain.VoiceAgent "Agent found"
// @Failure 404 {object} map[string]string "Agent not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/{id} [get]
func (h *AgentHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	agent, err := h.repoMgr.VoiceAgent().GetByID(r.Context(), id)
	if err != nil {
		if err.Error() == "voice agent not found: "+id {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent)
}

// GetAgents godoc
// @Summary List all agents
// @Description Retrieve a list of all voice agents, optionally filtered by tenant ID
// @Tags agents
// @Accept json
// @Produce json
// @Param tenant_id query string false "Filter agents by tenant ID"
// @Param include_disabled query boolean false "Include disabled agents" default(false)
// @Success 200 {array} domain.VoiceAgent "List of agents"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents [get]
func (h *AgentHandler) GetAgents(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	tenantID := r.URL.Query().Get("tenant_id")
	includeDisabledStr := r.URL.Query().Get("include_disabled")
	includeDisabled := includeDisabledStr == "true"

	var agents []*domain.VoiceAgent
	var err error

	if tenantID != "" {
		agents, err = h.repoMgr.VoiceAgent().GetByTenantID(r.Context(), tenantID, includeDisabled)
	} else {
		agents, err = h.repoMgr.VoiceAgent().GetAll(r.Context(), includeDisabled)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
}

// GetDefaultTenantAgents godoc
// @Summary List agents in default tenant
// @Description Retrieve agent_id and agent_name for agents under the default tenant ID
// @Tags agents
// @Accept json
// @Produce json
// @Success 200 {array} map[string]string "List of agents"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/default-tenant [get]
func (h *AgentHandler) GetDefaultTenantAgents(w http.ResponseWriter, r *http.Request) {
	defaultTenantID := localconfig.DefaultTenantID

	type agentSummary struct {
		AgentID            string `json:"agent_id"`
		AgentName          string `json:"agent_name"`
		ChannelPhoneNumber string `json:"channel_phone_number"`
		WatiTenantId       string `json:"wati_tenant_id"`
	}

	if h.agentService == nil {
		http.Error(w, "agent service not available", http.StatusInternalServerError)
		return
	}

	agentConfigs, err := h.agentService.GetAgentConfigByTenantID(r.Context(), defaultTenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := make([]agentSummary, 0, len(agentConfigs))
	for _, cfg := range agentConfigs {
		response = append(response, agentSummary{
			AgentID:            cfg.ID,
			AgentName:          cfg.Name,
			ChannelPhoneNumber: cfg.BusinessNumber,
			WatiTenantId:       localconfig.DefaultWatiTenantID,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateAgent godoc
// @Summary Update an existing agent
// @Description Update an existing voice agent's configuration
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID (UUID)" format(uuid)
// @Param agent body domain.UpdateVoiceAgentRequest true "Agent update request"
// @Success 200 {object} domain.VoiceAgent "Agent updated successfully"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 404 {object} map[string]string "Agent not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/{id} [put]
func (h *AgentHandler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req domain.UpdateVoiceAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// ✅ 获取现有 agent 用于 RAG 配置补充
	existingAgent, getErr := h.repoMgr.VoiceAgent().GetByID(r.Context(), id)
	if getErr != nil {
		if getErr.Error() == "voice agent not found: "+id {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		http.Error(w, getErr.Error(), http.StatusInternalServerError)
		return
	}

	// ✅ 自动设置 RAG 配置（如果未提供）
	h.ensureRAGConfig(r.Context(), &req.AgentConfig, existingAgent.TextAgentID)

	agent, err := h.repoMgr.VoiceAgent().Update(r.Context(), id, &req)
	if err != nil {
		if err.Error() == "voice agent not found: "+id {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync updated agent to cache (incremental update)
	if h.agentService != nil {
		if syncErr := h.agentService.SyncAgentUpdate(r.Context(), agent); syncErr != nil {
			logger.Base().Warn("failed to sync updated agent to cache",
				zap.Error(syncErr),
				zap.String("agent_id", agent.ID),
				zap.String("tenant_id", agent.VoiceTenantID),
			)
		}
	}

	// Register tools with MCP (Draft)
	if err := h.registerToolsWithMCP(r.Context(), agent, localconfig.AgentConfigModeDraft); err != nil {
		logger.Base().Warn("failed to register tools with MCP",
			zap.Error(err),
			zap.String("agent_id", agent.ID),
			zap.String("tenant_id", agent.VoiceTenantID),
			zap.String("mode", localconfig.AgentConfigModeDraft),
		)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent)
}

// PublishAgent godoc
// @Summary Publish agent configuration
// @Description Publish the current draft configuration to production
// @Tags agents
// @Accept json
// @Produce json
// @Param request body domain.PublishAgentRequest true "Publish request"
// @Success 200 {object} domain.PublishAgentResponse "Agent published successfully"
// @Failure 400 {object} map[string]string "Invalid request body or missing agent_id"
// @Failure 404 {object} map[string]string "Agent not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/publish [post]
func (h *AgentHandler) PublishAgent(w http.ResponseWriter, r *http.Request) {
	// Decode request
	var req domain.PublishAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.AgentID == "" {
		http.Error(w, "agent_id is required", http.StatusBadRequest)
		return
	}

	id := req.AgentID

	// Get agent to access draft config
	agent, err := h.repoMgr.VoiceAgent().GetByID(r.Context(), id)
	if err != nil {
		if err.Error() == "voice agent not found: "+id {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure draft config exists
	if agent.AgentConfig == nil {
		http.Error(w, "Cannot publish empty draft configuration", http.StatusBadRequest)
		return
	}

	// Deep copy Draft Config to Published Config
	// Using JSON marshal/unmarshal for safe deep copy
	configJSON, err := json.Marshal(agent.AgentConfig)
	if err != nil {
		logger.Base().Error("failed to marshal agent config for publishing",
			zap.Error(err),
			zap.String("agent_id", agent.ID),
		)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	var configToPublish domain.AgentConfigData
	if err := json.Unmarshal(configJSON, &configToPublish); err != nil {
		logger.Base().Error("failed to unmarshal agent config for publishing",
			zap.Error(err),
			zap.String("agent_id", agent.ID),
		)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Update PublishedAgentConfig in DB
	updatedAgent, err := h.repoMgr.VoiceAgent().PublishConfig(r.Context(), id, &configToPublish)
	if err != nil {
		logger.Base().Error("failed to publish agent config",
			zap.Error(err),
			zap.String("agent_id", id),
			zap.String("tenant_id", agent.VoiceTenantID),
		)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync to Cache (Both Prod and Draft keys will be updated)
	if h.agentService != nil {
		if syncErr := h.agentService.SyncAgentUpdate(r.Context(), updatedAgent); syncErr != nil {
			logger.Base().Warn("failed to sync published agent to cache",
				zap.Error(syncErr),
				zap.String("agent_id", updatedAgent.ID),
				zap.String("tenant_id", updatedAgent.VoiceTenantID),
			)
		}
	}

	// Register tools with MCP (Published)
	if err := h.registerToolsWithMCP(r.Context(), updatedAgent, localconfig.AgentConfigModePublished); err != nil {
		logger.Base().Warn("failed to register published tools with MCP",
			zap.Error(err),
			zap.String("agent_id", updatedAgent.ID),
			zap.String("tenant_id", updatedAgent.VoiceTenantID),
			zap.String("mode", localconfig.AgentConfigModePublished),
		)
	}

	// Return standardized response
	response := domain.PublishAgentResponse{
		ID:          updatedAgent.ID,
		PublishedAt: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DeleteAgent godoc
// @Summary Delete an agent
// @Description Delete a voice agent by its ID (soft delete)
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID (UUID)" format(uuid)
// @Success 204 "Agent deleted successfully"
// @Failure 404 {object} map[string]string "Agent not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/{id} [delete]
func (h *AgentHandler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Get agent info before deletion (needed for cache sync)
	agent, getErr := h.repoMgr.VoiceAgent().GetByID(r.Context(), id)
	if getErr != nil {
		http.Error(w, getErr.Error(), http.StatusInternalServerError)
		return
	}
	if agent == nil {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	err := h.repoMgr.VoiceAgent().Delete(r.Context(), id)
	if err != nil {
		if err.Error() == "voice agent not found: "+id {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync agent deletion to cache (incremental update)
	if h.agentService != nil {
		if syncErr := h.agentService.SyncAgentDelete(r.Context(), id, agent.VoiceTenantID); syncErr != nil {
			logger.Base().Warn("failed to sync deleted agent to cache",
				zap.Error(syncErr),
				zap.String("agent_id", id),
				zap.String("tenant_id", agent.VoiceTenantID),
			)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetAgentCount godoc
// @Summary Get agent count by tenant
// @Description Get the total count of agents for a specific tenant
// @Tags agents
// @Accept json
// @Produce json
// @Param tenant_id query string true "Tenant ID to count agents for"
// @Success 200 {object} map[string]int "Agent count" example({"count": 5})
// @Failure 400 {object} map[string]string "Missing tenant_id parameter"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/count [get]
func (h *AgentHandler) GetAgentCount(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id parameter is required", http.StatusBadRequest)
		return
	}

	count, err := h.repoMgr.VoiceAgent().CountByTenantID(r.Context(), tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]int{"count": count}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// CheckAgentExists godoc
// @Summary Check if agent exists
// @Description Check whether a voice agent with the specified ID exists
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID (UUID)" format(uuid)
// @Success 200 "Agent exists"
// @Failure 404 "Agent not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/{id} [head]
func (h *AgentHandler) CheckAgentExists(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	exists, err := h.repoMgr.VoiceAgent().Exists(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// GenerateJWT godoc
// @Summary Generate JWT token for agent
// @Description Generate a JWT token containing tenant ID and agent ID for API authentication
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID (UUID)" format(uuid)
// @Success 200 {object} map[string]string "JWT token generated successfully"
// @Failure 404 {object} map[string]string "Agent not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/{id}/jwt [get]
func (h *AgentHandler) GenerateJWT(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Get agent from database
	// Try to get by Voice Agent ID first
	agent, err := h.repoMgr.VoiceAgent().GetByID(r.Context(), id)
	if err != nil && agent == nil {
		agent, err = h.repoMgr.VoiceAgent().GetByTextAgentID(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	}

	if agent == nil {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	// Generate JWT token using services package
	jwt, err := httpadapter.GenerateAgentJWT(agent.VoiceTenantID, agent.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]string{
		"jwt":       jwt,
		"tenant_id": agent.VoiceTenantID,
		"agent_id":  agent.ID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetAgentByTenantAndTextAgent godoc
// @Summary Get agent by tenant ID and text agent ID
// @Description Retrieve a voice agent by tenant ID and text agent ID
// @Tags agents
// @Accept json
// @Produce json
// @Param tenant_id query string true "Tenant ID"
// @Param text_agent_id query string true "Text Agent ID"
// @Success 200 {object} domain.VoiceAgent "Agent found"
// @Failure 400 {object} map[string]string "Missing required parameters"
// @Failure 404 {object} map[string]string "Agent not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/by-text-agent [get]
func (h *AgentHandler) GetAgentByTenantAndTextAgent(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	textAgentID := r.URL.Query().Get("text_agent_id")

	if tenantID == "" {
		http.Error(w, "tenant_id parameter is required", http.StatusBadRequest)
		return
	}

	if textAgentID == "" {
		http.Error(w, "text_agent_id parameter is required", http.StatusBadRequest)
		return
	}

	agent, err := h.repoMgr.VoiceAgent().GetByTenantIDAndTextAgentID(r.Context(), tenantID, textAgentID)
	if err != nil {
		if err.Error() == fmt.Sprintf("voice agent not found for tenant %s and text agent %s", tenantID, textAgentID) {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent)
}

// QuickCreateAgent godoc
// @Summary Quick create agent from brandkit
// @Description Quickly create an agent using tenant_id and text_agent_id. Auto-creates tenant if not exists, fetches brandkit config, and sets up RAG configuration.
// @Tags agents
// @Accept json
// @Produce json
// @Param request body domain.QuickCreateAgentRequest true "Quick create request"
// @Success 201 {object} domain.VoiceAgent "Agent created successfully"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/agents/quick-create [post]
func (h *AgentHandler) QuickCreateAgent(w http.ResponseWriter, r *http.Request) {
	// Parse request
	var req domain.QuickCreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.TenantID == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}

	if req.TextAgentID == "" {
		http.Error(w, "text_agent_id is required", http.StatusBadRequest)
		return
	}

	logger.Base().Info("quick create agent request",
		zap.String("tenant_id", req.TenantID),
		zap.String("text_agent_id", req.TextAgentID),
	)

	// Step 0: Check if agent with same tenant_id and text_agent_id already exists
	existingAgent, err := h.repoMgr.VoiceAgent().GetByTenantIDAndTextAgentID(r.Context(), req.TenantID, req.TextAgentID)
	if err == nil && existingAgent != nil {
		// Agent already exists, return it directly
		logger.Base().Info("agent already exists, returning existing",
			zap.String("tenant_id", req.TenantID),
			zap.String("text_agent_id", req.TextAgentID),
			zap.String("agent_id", existingAgent.ID),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(existingAgent)
		return
	}
	// If error is "not found", continue with creation; otherwise return error
	if err != nil && err.Error() != fmt.Sprintf("voice agent not found for tenant %s and text agent %s", req.TenantID, req.TextAgentID) {
		logger.Base().Error("error checking existing agent",
			zap.Error(err),
			zap.String("tenant_id", req.TenantID),
			zap.String("text_agent_id", req.TextAgentID),
		)
		http.Error(w, fmt.Sprintf("Error checking existing agent: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Base().Info("no existing agent found, creating new",
		zap.String("tenant_id", req.TenantID),
		zap.String("text_agent_id", req.TextAgentID),
	)
	agentName := "Voice Assistant"

	// Step 1: Quick creation with default/template config (fast path)
	// Try to get brandkit name quickly (non-blocking, but fast)
	brandkit, _ := FetchBrandkit(req.TextAgentID)
	var textAgentConfig *mapping.TextAgentConfig

	if brandkit != nil {
		agentName = brandkit.Name
		logger.Base().Info("fetched brandkit",
			zap.String("brandkit_name", brandkit.Name),
			zap.String("text_agent_id", req.TextAgentID),
		)

		// If brandkit exists, fetch text agent config to use its instructions
		if h.mappingService != nil {
			textAgentConfig, err = h.mappingService.GetTextAgent(req.TextAgentID)
			logger.Base().Info("fetched text agent config", zap.Any("text_agent_config", textAgentConfig))
			if err != nil {
				logger.Base().Error("failed to fetch text agent config",
					zap.Error(err),
					zap.String("text_agent_id", req.TextAgentID),
				)
			}
			// Ignore errors, continue with default if fetch fails
		}
	}

	// Fetch Template if needed (for quick creation)
	var agentTemplate *mapping.TemplateV2
	if req.TemplateID != "" && h.mappingService != nil {
		agentTemplate, err = h.mappingService.GetTemplate(req.TemplateID)
		logger.Base().Info("fetched template", zap.Any("template", agentTemplate), zap.String("template_id", req.TemplateID))
		if err != nil {
			logger.Base().Error("failed to fetch template", zap.Error(err), zap.String("template_id", req.TemplateID))
		}
		// Ignore errors, continue with default if template fetch fails
	}

	// Use default config for quick creation
	agentConfig := domain.GetDefaultAgentConfig()

	// Priority: text agent config > template > default
	if textAgentConfig != nil && textAgentConfig.Instructions != "" {
		// Use text agent instructions if available
		agentConfig.PromptConfig.RealtimeTemplate = textAgentConfig.Instructions
		agentConfig.OutboundPromptConfig.RealtimeTemplate = textAgentConfig.Instructions
		agentName = textAgentConfig.Name
		logger.Base().Info("using text agent instructions for quick creation",
			zap.String("agent_name", agentName),
			zap.String("text_agent_id", req.TextAgentID),
		)
	} else if agentTemplate != nil {
		// Fallback to template if text agent config not available
		agentName = agentTemplate.Name
		agentConfig.PromptConfig.RealtimeTemplate = agentTemplate.Instructions
		agentConfig.OutboundPromptConfig.RealtimeTemplate = agentTemplate.Instructions
		if agentTemplate.VoiceInstructions != "" {
			agentConfig.PromptConfig.RealtimeTemplate = agentTemplate.VoiceInstructions
			agentConfig.OutboundPromptConfig.RealtimeTemplate = agentTemplate.VoiceInstructions
		}
		logger.Base().Info("using template instructions for quick creation",
			zap.String("agent_name", agentName),
			zap.String("template_id", req.TemplateID),
		)
	}

	// Step 2: Ensure tenant exists
	if h.repoMgr == nil {
		http.Error(w, "Tenant repository not available", http.StatusInternalServerError)
		return
	}

	tenant, err := h.ensureTenantExists(r.Context(), req.TenantID, agentName)
	if err != nil {
		logger.Base().Error("failed to ensure tenant exists",
			zap.Error(err),
			zap.String("tenant_id", req.TenantID),
		)
		http.Error(w, fmt.Sprintf("Failed to ensure tenant: %v", err), http.StatusInternalServerError)
		return
	}
	logger.Base().Info("tenant ensured", zap.String("tenant_id", tenant.TenantID))

	h.ensureRAGConfig(r.Context(), &agentConfig, &req.TextAgentID)

	// Step 3: Create agent quickly with default config
	createReq := &domain.CreateVoiceAgentRequest{
		VoiceTenantID: req.TenantID,
		AgentName:     agentName,
		TextAgentID:   &req.TextAgentID,
		AgentConfig:   agentConfig,
	}

	agent, err := h.repoMgr.VoiceAgent().Create(r.Context(), createReq)
	if err != nil {
		logger.Base().Error("failed to create agent",
			zap.Error(err),
			zap.String("tenant_id", req.TenantID),
			zap.String("text_agent_id", req.TextAgentID),
		)
		http.Error(w, fmt.Sprintf("Failed to create agent: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Base().Info("agent created quickly",
		zap.String("agent_name", agent.AgentName),
		zap.String("agent_id", agent.ID),
		zap.String("tenant_id", agent.VoiceTenantID),
	)

	// Step 4: Sync to cache
	if h.agentService != nil {
		if syncErr := h.agentService.SyncAgentCreate(r.Context(), agent); syncErr != nil {
			logger.Base().Warn("failed to sync created agent to cache",
				zap.Error(syncErr),
				zap.String("agent_id", agent.ID),
				zap.String("tenant_id", agent.VoiceTenantID),
			)
		}
	}

	// Step 5: Register tools with MCP (Draft)
	if err := h.registerToolsWithMCP(r.Context(), agent, localconfig.AgentConfigModeDraft); err != nil {
		logger.Base().Warn("failed to register tools with MCP",
			zap.Error(err),
			zap.String("agent_id", agent.ID),
			zap.String("tenant_id", agent.VoiceTenantID),
			zap.String("mode", localconfig.AgentConfigModeDraft),
		)
	}

	// Step 6: Return response immediately
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(agent)

	// Step 7: Asynchronously update config with AI-generated content
	// go h.asyncUpdateAgentConfig(r.Context(), agent.ID, req.TextAgentID, req.TemplateID, brandkit, agentTemplate)
}

// ensureTenantExists checks if tenant exists, creates if not
func (h *AgentHandler) ensureTenantExists(ctx context.Context, tenantID string, agentName string) (*domain.VoiceTenant, error) {
	// Try to get existing tenant
	tenant, err := h.repoMgr.VoiceTenant().GetByTenantID(ctx, tenantID)
	if err == nil {
		return tenant, nil
	}

	// If not found, create new tenant
	logger.Base().Info("tenant not found, creating new tenant", zap.String("tenant_id", tenantID))

	// Use tenantID-agentName format for tenant name
	tenantName := agentName

	createReq := &domain.CreateVoiceTenantRequest{
		TenantID:   tenantID,
		TenantName: tenantName,
		AstraKey:   fmt.Sprintf("astra-key-%s", tenantID), // Auto-generate astra key
	}

	tenant, err = h.repoMgr.VoiceTenant().Create(ctx, createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	logger.Base().Info("created new tenant",
		zap.String("tenant_id", tenant.TenantID),
		zap.String("tenant_name", tenant.TenantName),
	)
	return tenant, nil
}

// ensureRAGConfig ensures RAG configuration is set with default values if not provided
func (h *AgentHandler) ensureRAGConfig(ctx context.Context, agentConfig **domain.AgentConfigData, textAgentID *string) {
	if *agentConfig == nil {
		*agentConfig = &domain.AgentConfigData{}
	}

	// 如果 RAGConfig 为 nil，创建一个新的
	if (*agentConfig).RAGConfig == nil {
		(*agentConfig).RAGConfig = &domain.RAGConfigData{
			Enabled:    true,
			Timeout:    30,
			MaxRetries: 3,
		}
	}

	ragConfig := (*agentConfig).RAGConfig

	// Dify API
	var dbToken string
	if textAgentID != nil && *textAgentID != "" {
		token, err := h.repoMgr.VoiceAgent().GetAgentAPIKeyByPlatformAgentID(ctx, *textAgentID, "live")
		if err != nil {
			logger.Base().Warn("[Agent] failed to get RAG token from DB",
				zap.Error(err),
				zap.String("text_agent_id", *textAgentID),
			)
		} else if token != "" {
			dbToken = token
			logger.Base().Info("[Agent] retrieved RAG token from DB",
				zap.String("text_agent_id", *textAgentID),
			)
		}
	}

	if dbToken != "" {
		if ragConfig.Token == "" {
			ragConfig.Token = dbToken
			logger.Base().Info("[Agent] Using DB token (Dify format)")
		}
		if ragConfig.BaseURL == "" {
			ragConfig.BaseURL = os.Getenv("RAG_API_SERVICE_URL")
			logger.Base().Info("[Agent] Auto-set RAG BaseURL from RAG_API_SERVICE_URL",
				zap.String("rag_base_url", ragConfig.BaseURL),
			)
		}
	} else if textAgentID != nil && *textAgentID != "" {
		if ragConfig.Token == "" {
			ragConfig.Token = *textAgentID
			logger.Base().Info("[Agent] Using TextAgentID as token (new format)",
				zap.String("text_agent_id", *textAgentID),
			)
		}
		if ragConfig.BaseURL == "" {
			ragCfg := config.LoadRagAPIServiceConfig()
			ragConfig.BaseURL = ragCfg.RagAPIServiceURL
			logger.Base().Info("[Agent] Auto-set RAG BaseURL from config",
				zap.String("rag_base_url", ragConfig.BaseURL),
			)
		}
	}

	if !ragConfig.Enabled {
		ragConfig.Enabled = true
	}
	if ragConfig.Timeout == 0 {
		ragConfig.Timeout = 30
	}
	if ragConfig.MaxRetries == 0 {
		ragConfig.MaxRetries = 3
	}
}

// buildAgentConfigFromGenerated builds AgentConfigData from generated config
func (h *AgentHandler) buildAgentConfigFromGenerated(generated *GeneratedAgentConfig) *domain.AgentConfigData {
	config := &domain.AgentConfigData{
		Persona:       generated.Persona,
		Tone:          generated.Tone,
		Language:      generated.Language,
		DefaultAccent: generated.DefaultAccent,
		Voice:         generated.Voice,
		Speed:         generated.Speed,
		Services:      generated.Services,
		Expertise:     generated.Expertise,
		PromptConfig: &domain.PromptConfigData{
			GreetingTemplate:   generated.GreetingTemplate,
			RealtimeTemplate:   generated.RealtimeTemplate,
			SystemInstructions: generated.SystemInstructions,
		},
	}

	return config
}

// registerToolsWithMCP registers tools with MCP service
func (h *AgentHandler) registerToolsWithMCP(ctx context.Context, agent *domain.VoiceAgent, mode string) error {
	var targetConfig *domain.AgentConfigData
	if mode == localconfig.AgentConfigModePublished {
		targetConfig = agent.PublishedAgentConfig
	} else {
		targetConfig = agent.AgentConfig
	}

	// Skip if no agent config
	if targetConfig == nil {
		return nil
	}

	logger.Base().Info("registering tools with MCP",
		zap.String("agent_id", agent.ID),
		zap.String("mode", mode),
		zap.Int("inbound_actions", len(targetConfig.IntegratedActions)),
		zap.Int("outbound_actions", len(targetConfig.OutboundIntegratedActions)),
		zap.String("tenant_id", agent.VoiceTenantID),
	)

	// 1. Register Inbound Actions
	var inboundActions []mcp.RegisterActionItem
	for _, action := range targetConfig.IntegratedActions {
		inboundActions = append(inboundActions, mcp.RegisterActionItem{
			ActionID: action.ActionID,
			AtID:     action.AtID,
		})
	}
	// Ensure non-nil slice for empty JSON array
	if inboundActions == nil {
		inboundActions = make([]mcp.RegisterActionItem, 0)
	}

	// Determine Agent ID to use (prefer TextAgentID if available)
	registerAgentID := agent.ID
	if agent.TextAgentID != nil && *agent.TextAgentID != "" {
		registerAgentID = *agent.TextAgentID
		logger.Base().Info("using text agent id for tool registration",
			zap.String("register_agent_id", registerAgentID),
			zap.String("voice_agent_id", agent.ID),
		)
	}

	reqInbound := mcp.RegisterToolsRequest{
		AgentID:  registerAgentID,
		Mode:     mode,
		Actions:  inboundActions,
		TenantID: agent.VoiceTenantID,
		Modality: mcp.ModalityVoiceInbound,
	}

	if _, err := h.composioService.RegisterTools(ctx, reqInbound); err != nil {
		return fmt.Errorf("failed to register inbound tools: %w", err)
	}

	// 2. Register Outbound Actions
	var outboundActions []mcp.RegisterActionItem
	for _, action := range targetConfig.OutboundIntegratedActions {
		outboundActions = append(outboundActions, mcp.RegisterActionItem{
			ActionID: action.ActionID,
			AtID:     action.AtID,
		})
	}
	// Ensure non-nil slice for empty JSON array
	if outboundActions == nil {
		outboundActions = make([]mcp.RegisterActionItem, 0)
	}

	reqOutbound := mcp.RegisterToolsRequest{
		AgentID:  registerAgentID,
		Mode:     mode,
		Actions:  outboundActions,
		TenantID: agent.VoiceTenantID,
		Modality: mcp.ModalityVoiceOutbound,
	}

	if _, err := h.composioService.RegisterTools(ctx, reqOutbound); err != nil {
		return fmt.Errorf("failed to register outbound tools: %w", err)
	}

	return nil
}

// SetupAgentRoutes sets up all agent-related routes
func (h *AgentHandler) SetupAgentRoutes(router *mux.Router) {

	// Agent CRUD routes
	router.HandleFunc("/agents", h.CreateAgent).Methods("POST")
	router.HandleFunc("/agents", h.GetAgents).Methods("GET")
	router.HandleFunc("/agents/default-tenant", h.GetDefaultTenantAgents).Methods("GET")
	router.HandleFunc("/agents/by-text-agent", h.GetAgentByTenantAndTextAgent).Methods("GET")
	router.HandleFunc("/agents/publish", h.PublishAgent).Methods("POST") // Changed from /{id}/publish to /publish
	router.HandleFunc("/agents/{id}", h.GetAgent).Methods("GET")
	router.HandleFunc("/agents/{id}", h.UpdateAgent).Methods("PUT")
	router.HandleFunc("/agents/{id}", h.DeleteAgent).Methods("DELETE")
	router.HandleFunc("/agents/{id}", h.CheckAgentExists).Methods("HEAD")
	router.HandleFunc("/agents/count", h.GetAgentCount).Methods("GET")
	router.HandleFunc("/agents/{id}/jwt", h.GenerateJWT).Methods("GET")

	// Quick create route
	router.HandleFunc("/agents/quick-create", h.QuickCreateAgent).Methods("POST")

	logger.Base().Info("agent routes registered")
}
