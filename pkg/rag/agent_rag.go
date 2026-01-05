package rag

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/services/agent"
	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// AgentRAGProcessor handles RAG processing for different agents
type AgentRAGProcessor struct {
	agentService *agent.AgentService
	ragClients   map[string]*AgentRAGClient
	translator   Translator
	mutex        sync.RWMutex
}

// AgentRAGClient wraps RAG client with agent-specific configuration
type AgentRAGClient struct {
	Agent  *config.AgentConfig
	Client *RAGClient
}

// NewAgentRAGProcessor creates a new agent-based RAG processor
func NewAgentRAGProcessor(agentService *agent.AgentService, translator Translator) *AgentRAGProcessor {
	processor := &AgentRAGProcessor{
		agentService: agentService,
		ragClients:   make(map[string]*AgentRAGClient),
		translator:   translator,
	}

	// Initialize RAG clients for all active agents
	processor.initializeRAGClients()

	return processor
}

// initializeRAGClients initializes RAG clients for all active agents
func (p *AgentRAGProcessor) initializeRAGClients() {
	agents, err := p.agentService.GetActiveAgents()
	if err != nil {
		logger.Base().Error("Failed to load active agents")
		return
	}
	logger.Base().Debug("Initializing RAG clients for active agents", zap.Int("agent_count", len(agents)))

	for _, agent := range agents {
		if agent.RAGConfig != nil && agent.RAGConfig.Enabled {
			ragClient := NewRAGClient(
				agent.RAGConfig.BaseURL,
				agent.RAGConfig.Token,
			)

			p.ragClients[agent.ID] = &AgentRAGClient{
				Agent:  agent,
				Client: ragClient,
			}

			logger.Base().Debug("Initialized RAG client for agent", zap.String("id", agent.ID), zap.String("company_name", agent.CompanyName), zap.Any("rag_config", agent.RAGConfig))
		} else {
			logger.Base().Debug("RAG disabled for agent", zap.String("id", agent.ID), zap.String("company_name", agent.CompanyName))
		}
	}
}

// ProcessUserInput processes user input with agent-specific RAG
func (p *AgentRAGProcessor) ProcessUserInput(userInput, connectionID, agentID string) (shouldCallRAG bool, ragContext string, processedInput string) {
	return p.ProcessUserInputWithChannelType(userInput, connectionID, agentID, "")
}

// ProcessUserInputWithChannelType processes user input with agent-specific RAG and ChannelType support
func (p *AgentRAGProcessor) ProcessUserInputWithChannelType(userInput, connectionID, agentID string, channelType domain.ChannelType) (shouldCallRAG bool, ragContext string, processedInput string) {
	if userInput == "" {
		return false, "", userInput
	}

	logger.Base().Debug("Processing user input for agent", zap.String("agent_id", agentID))

	// Always get fresh agent configuration to ensure we have the latest RAG settings
	// Always use GetAgentConfigWithChannelType (unified entry point)
	// Empty channelType will fallback to GetAgentConfig behavior (Published with Draft fallback)
	agent, err := p.agentService.GetAgentConfigWithChannelType(context.Background(), agentID, channelType)
	if err != nil {
		logger.Base().Error("Failed to load agent", zap.String("agent_id", agentID))
		return false, "", userInput
	}

	// Get agent-specific RAG client with fresh configuration
	agentRAGClient, exists := p.GetRAGClient(agentID)
	if !exists || agentRAGClient == nil {
		logger.Base().Error("Failed to get RAG client for agent", zap.String("agent_id", agentID))
		return false, "", userInput
	}

	// // Check if translator is available
	// if !p.translator.IsAvailable() {
	// 	logger.Base().Warn("Translator not available, skipping RAG processing")
	// 	return false, "", userInput
	// }

	// Stage 1: Translate user question to English query for RAG
	englishQuery, err := p.translator.TranslateToEnglishQuery(userInput)
	if err != nil {
		logger.Base().Error("Translation to English failed")
		return false, "", userInput
	}

	logger.Base().Info("🌐 Translated query for agent", zap.String("agent_id", agentID), zap.String("englishquery", englishQuery))

	// ✅ Call agent-specific RAG with timeout control (3 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Channel to receive RAG result
	type ragResult struct {
		answer string
		err    error
	}
	resultChan := make(chan ragResult, 1)

	// Async RAG query with timeout
	go func() {
		answer, err := agentRAGClient.Client.QueryRAG(ctx, englishQuery, connectionID, "")
		resultChan <- ragResult{answer: answer, err: err}
	}()

	// Wait for result or timeout
	var ragAnswer string
	select {
	case result := <-resultChan:
		if result.err != nil {
			logger.Base().Error("RAG query failed for agent", zap.Error(result.err))
			return false, "", userInput
		}
		ragAnswer = result.answer
		answerPreview := ragAnswer
		if len(answerPreview) > 100 {
			answerPreview = answerPreview[:100] + "..."
		}
		logger.Base().Info("RAG answer received", zap.String("agent_id", agentID), zap.String("answer_preview", answerPreview))

	case <-ctx.Done():
		logger.Base().Info("⏰ RAG query timeout (3s) for agent , proceeding without RAG context", zap.String("agent_id", agentID))
		return false, "", userInput
	}

	// Create agent-specific RAG context
	ragContext = fmt.Sprintf(`[%s KNOWLEDGE BASE CONTEXT]
User originally asked: "%s"
English query used for knowledge base: "%s"

%s Knowledge Base Response:
%s

[END %s KNOWLEDGE BASE CONTEXT]`,
		agent.CompanyName,
		userInput,
		englishQuery,
		agent.CompanyName,
		ragAnswer,
		agent.CompanyName)

	logger.Base().Info("RAG context generated", zap.String("agent_id", agentID), zap.Int("context_length", len(ragAnswer)))

	// Return the original user input with agent-specific RAG context
	return true, ragContext, userInput
}

// GetRAGClient returns the RAG client for a specific agent with fresh agent configuration
func (p *AgentRAGProcessor) GetRAGClient(agentID string) (*AgentRAGClient, bool) {
	// Always get fresh agent configuration first
	// Use empty channelType to get Published config (default behavior)
	agent, err := p.agentService.GetAgentConfigWithChannelType(context.Background(), agentID, "")
	if err != nil {
		logger.Base().Error("Failed to load fresh agent", zap.String("agent_id", agentID))
		return nil, false
	}

	// Check if RAG is enabled
	if agent.RAGConfig == nil || !agent.RAGConfig.Enabled {
		logger.Base().Warn("RAG disabled for agent", zap.String("agent_id", agentID))
		return nil, false
	}

	// Always create RAGClient with fresh agent config
	logger.Base().Info("Creating RAGClient for agent with fresh config", zap.String("agent_id", agentID))
	newRAGClient := NewRAGClient(agent.RAGConfig.BaseURL, agent.RAGConfig.Token)

	// Update cache with fresh agent config
	p.mutex.Lock()
	p.ragClients[agentID] = &AgentRAGClient{
		Agent:  agent, // 使用最新的 agent 配置
		Client: newRAGClient,
	}
	p.mutex.Unlock()

	return &AgentRAGClient{
		Agent:  agent,
		Client: newRAGClient,
	}, true
}

// UpdateAgentRAGConfig updates RAG configuration for a specific agent
func (p *AgentRAGProcessor) UpdateAgentRAGConfig(agentID string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Load updated agent configuration
	// Use empty channelType to get Published config (default behavior)
	agent, err := p.agentService.GetAgentConfigWithChannelType(context.Background(), agentID, "")
	if err != nil {
		return fmt.Errorf("failed to load agent %s: %w", agentID, err)
	}

	// Remove old client
	delete(p.ragClients, agentID)

	// Add new client if RAG is enabled
	if agent.RAGConfig != nil && agent.RAGConfig.Enabled {
		ragClient := NewRAGClient(agent.RAGConfig.BaseURL, agent.RAGConfig.Token)
		p.ragClients[agentID] = &AgentRAGClient{
			Agent:  agent,
			Client: ragClient,
		}
		logger.Base().Info("Updated RAG client for agent", zap.String("agent_id", agentID))
	} else {
		logger.Base().Info("🗑 Removed RAG client for agent (disabled)", zap.String("agent_id", agentID))
	}

	return nil
}

// RefreshAllAgents refreshes RAG clients for all agents
func (p *AgentRAGProcessor) RefreshAllAgents() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Clear existing clients
	p.ragClients = make(map[string]*AgentRAGClient)

	// Reinitialize
	p.initializeRAGClients()

	logger.Base().Info("Refreshed all agent RAG clients")
	return nil
}

// GetActiveAgents returns all agents with active RAG configurations
func (p *AgentRAGProcessor) GetActiveAgents() ([]*config.AgentConfig, error) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	agents := make([]*config.AgentConfig, 0, len(p.ragClients))
	for _, client := range p.ragClients {
		agents = append(agents, client.Agent)
	}

	return agents, nil
}
