package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/jinzhu/copier"
	"go.uber.org/zap"
)

var (
	instance *AgentCache
	once     sync.Once
)

// AgentCache provides thread-safe agent management with database-backed cache
type AgentCache struct {
	agents         map[string]*config.AgentConfig // id -> agent
	textAgentIndex map[string]string              // text_agent_id -> id
	mutex          sync.RWMutex
	updateChan     chan []*config.AgentConfig
	ctx            context.Context
	cancel         context.CancelFunc
	isStarted      bool
	startMutex     sync.Mutex
}

// NewAgentCache returns the agent cache (internally managed as singleton)
func NewAgentCache() *AgentCache {
	once.Do(func() {
		instance = createAgentCache()
	})
	return instance
}

// createAgentCache is the internal constructor for the singleton
func createAgentCache() *AgentCache {
	ctx, cancel := context.WithCancel(context.Background())

	cache := &AgentCache{
		agents:         make(map[string]*config.AgentConfig),
		textAgentIndex: make(map[string]string),
		mutex:          sync.RWMutex{},
		updateChan:     make(chan []*config.AgentConfig, 1000), // 缓冲1000个更新请求
		ctx:            ctx,
		cancel:         cancel,
	}

	// Start async update processor
	cache.startAsyncProcessor()

	logger.Base().Info("AgentCache initialized (empty cache, waiting for database load)")
	return cache
}

// GetAgent retrieves an agent by ID or TextAgentID (thread-safe read)
// Supports both agent.ID and agent.TextAgentID for lookup
func (c *AgentCache) GetAgent(idOrTextAgentID string) (*config.AgentConfig, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// First, try to find by direct ID
	agent, exists := c.agents[idOrTextAgentID]
	if exists {
		return c.copyAgent(agent), nil
	}

	// If not found, try to find by TextAgentID
	if actualID, exists := c.textAgentIndex[idOrTextAgentID]; exists {
		if agent, ok := c.agents[actualID]; ok {
			return c.copyAgent(agent), nil
		}
	}

	return nil, fmt.Errorf("agent not found: %s", idOrTextAgentID)
}

// GetAllAgents retrieves all agents (thread-safe read)
func (c *AgentCache) GetAllAgents() ([]*config.AgentConfig, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	agents := make([]*config.AgentConfig, 0, len(c.agents))
	for _, agent := range c.agents {
		agents = append(agents, c.copyAgent(agent))
	}

	return agents, nil
}

// GetActiveAgents retrieves only active agents (thread-safe read)
func (c *AgentCache) GetActiveAgents() ([]*config.AgentConfig, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	agents := make([]*config.AgentConfig, 0)
	for _, agent := range c.agents {
		if agent.IsActive {
			agents = append(agents, c.copyAgent(agent))
		}
	}

	return agents, nil
}

// ListAgentIDs returns all agent IDs (thread-safe read)
func (c *AgentCache) ListAgentIDs() ([]string, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	ids := make([]string, 0, len(c.agents))
	for id := range c.agents {
		ids = append(ids, id)
	}

	return ids, nil
}

// UpdateAgent updates an existing agent configuration (thread-safe write)
func (c *AgentCache) UpdateAgent(agent *config.AgentConfig) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if agent exists
	oldAgent, exists := c.agents[agent.ID]
	if !exists {
		return fmt.Errorf("agent not found: %s", agent.ID)
	}

	// Update timestamp
	agent.UpdatedAt = time.Now()

	// Remove old text_agent_id from index if it changed
	if oldAgent.TextAgentID != "" && oldAgent.TextAgentID != agent.TextAgentID {
		delete(c.textAgentIndex, oldAgent.TextAgentID)
	}

	// Add new text_agent_id to index
	if agent.TextAgentID != "" {
		c.textAgentIndex[agent.TextAgentID] = agent.ID
	}

	// Store the agent (make a copy to prevent external modifications)
	c.agents[agent.ID] = c.copyAgent(agent)

	logger.Base().Info("Agent updated", zap.String("agent_id", agent.ID), zap.String("agent_name", agent.Name))
	return nil
}

// CreateAgent creates a new agent configuration (thread-safe write)
func (c *AgentCache) CreateAgent(agent *config.AgentConfig) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if agent already exists
	if _, exists := c.agents[agent.ID]; exists {
		return fmt.Errorf("agent already exists: %s", agent.ID)
	}

	// Set timestamps
	now := time.Now()
	agent.CreatedAt = now
	agent.UpdatedAt = now

	// Add text_agent_id to index
	if agent.TextAgentID != "" {
		c.textAgentIndex[agent.TextAgentID] = agent.ID
	}

	// Store the agent (make a copy to prevent external modifications)
	c.agents[agent.ID] = c.copyAgent(agent)

	logger.Base().Info("Agent created", zap.String("agent_id", agent.ID), zap.String("agent_name", agent.Name))
	return nil
}

// DeleteAgent removes an agent configuration (thread-safe write)
func (c *AgentCache) DeleteAgent(id string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if agent exists
	agent, exists := c.agents[id]
	if !exists {
		return fmt.Errorf("agent not found: %s", id)
	}

	// Remove from text_agent_id index
	if agent.TextAgentID != "" {
		delete(c.textAgentIndex, agent.TextAgentID)
	}

	// Delete the agent
	delete(c.agents, id)

	logger.Base().Info("Agent deleted", zap.String("agent_id", id))
	return nil
}

// UpsertAgent creates or updates an agent configuration (thread-safe write)
func (c *AgentCache) UpsertAgent(agent *config.AgentConfig) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	now := time.Now()

	// Check if agent exists
	if oldAgent, exists := c.agents[agent.ID]; exists {
		// Update existing agent
		agent.UpdatedAt = now

		// Remove old text_agent_id from index if it changed
		if oldAgent.TextAgentID != "" && oldAgent.TextAgentID != agent.TextAgentID {
			delete(c.textAgentIndex, oldAgent.TextAgentID)
		}

		logger.Base().Info("Agent updated", zap.String("agent_id", agent.ID), zap.String("agent_name", agent.Name))
	} else {
		// Create new agent
		agent.CreatedAt = now
		agent.UpdatedAt = now
		logger.Base().Info("Agent created", zap.String("agent_id", agent.ID), zap.String("agent_name", agent.Name))
	}

	// Add text_agent_id to index
	if agent.TextAgentID != "" {
		c.textAgentIndex[agent.TextAgentID] = agent.ID
	}

	// Store the agent (make a copy to prevent external modifications)
	c.agents[agent.ID] = c.copyAgent(agent)

	return nil
}

// GetAgentCount returns the total number of agents (thread-safe read)
func (c *AgentCache) GetAgentCount() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return len(c.agents)
}

// GetActiveAgentCount returns the number of active agents (thread-safe read)
func (c *AgentCache) GetActiveAgentCount() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	count := 0
	for _, agent := range c.agents {
		if agent.IsActive {
			count++
		}
	}

	return count
}

// copyAgent creates a deep copy of an agent configuration to prevent external modifications
// Uses github.com/jinzhu/copier for automatic deep copy - no need to manually update when adding new fields
func (c *AgentCache) copyAgent(original *config.AgentConfig) *config.AgentConfig {
	if original == nil {
		return nil
	}

	// Use copier for automatic deep copy
	// This will copy all fields including newly added ones automatically
	var copy config.AgentConfig
	if err := copier.CopyWithOption(&copy, original, copier.Option{DeepCopy: true}); err != nil {
		logger.Base().Warn("Failed to copy agent config", zap.Error(err))
		return original // Fallback to returning original if copy fails
	}

	return &copy
}

// UpdateAgentsAsync performs asynchronous bulk update with all provided agents
// This is the single method for external systems to update agents
func (c *AgentCache) UpdateAgentsAsync(agents []*config.AgentConfig) error {
	// If nil is provided, initialize to empty slice to avoid failure
	if agents == nil {
		agents = make([]*config.AgentConfig, 0)
	}

	// Check if cache is shutdown
	select {
	case <-c.ctx.Done():
		return fmt.Errorf("cache is shutdown")
	default:
	}

	// Send to async processor (non-blocking)
	select {
	case c.updateChan <- agents:
		return nil
	case <-c.ctx.Done():
		return fmt.Errorf("cache is shutdown")
	default:
		return fmt.Errorf("update queue is full, please try again later")
	}
}

// startAsyncProcessor starts the background goroutine to process updates
func (c *AgentCache) startAsyncProcessor() {
	c.startMutex.Lock()
	defer c.startMutex.Unlock()

	if c.isStarted {
		return
	}

	c.isStarted = true

	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case agents := <-c.updateChan:
				c.processUpdate(agents)
			}
		}
	}()
}

// processUpdate handles the actual update logic
func (c *AgentCache) processUpdate(agents []*config.AgentConfig) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	oldCount := len(c.agents)

	// Create new agent map and text_agent_id index
	newAgents := make(map[string]*config.AgentConfig)
	newTextAgentIndex := make(map[string]string)

	// Process provided agents from database
	seenIDs := make(map[string]bool) // Track duplicates within this batch

	for _, agent := range agents {
		// Validate agent
		if err := c.validateAgent(agent); err != nil {
			logger.Base().Warn("Skipping invalid agent in update batch", zap.Error(err))
			continue
		}

		// Check for duplicate IDs in the input (batch-specific logic)
		if seenIDs[agent.ID] {
			continue
		}
		seenIDs[agent.ID] = true

		// Set update timestamp
		agent.UpdatedAt = time.Now()

		// Store deep copy to prevent external modifications
		copiedAgent := c.copyAgent(agent)
		newAgents[agent.ID] = copiedAgent

		// Build text_agent_id index
		if copiedAgent.TextAgentID != "" {
			newTextAgentIndex[copiedAgent.TextAgentID] = copiedAgent.ID
		}
	}

	// Atomic replacement of both agents map and text_agent_id index
	c.agents = newAgents
	c.textAgentIndex = newTextAgentIndex

	newCount := len(c.agents)
	logger.Base().Info("Async update completed", zap.Int("old_count", oldCount), zap.Int("new_count", newCount), zap.Int("provided_count", len(agents)))
}

// validateAgent validates a single agent configuration
func (c *AgentCache) validateAgent(agent *config.AgentConfig) error {
	if agent == nil {
		return fmt.Errorf("agent is nil")
	}

	// Check required fields
	if agent.ID == "" {
		return fmt.Errorf("agent has empty ID")
	}

	if agent.Name == "" {
		return fmt.Errorf("agent %s has empty Name", agent.ID)
	}

	return nil
}

// validateAgents validates a slice of agents (Deprecated: used for full batch validation)
func (c *AgentCache) validateAgents(agents []*config.AgentConfig) error {
	if len(agents) == 0 {
		return fmt.Errorf("no agents provided")
	}

	seenIDs := make(map[string]bool)

	for i, agent := range agents {
		if err := c.validateAgent(agent); err != nil {
			return fmt.Errorf("agent at index %d invalid: %w", i, err)
		}

		// Check for duplicate IDs in the input
		if seenIDs[agent.ID] {
			return fmt.Errorf("duplicate agent ID found: %s", agent.ID)
		}
		seenIDs[agent.ID] = true
	}

	return nil
}

// Shutdown gracefully shuts down the agent cache
func (c *AgentCache) Shutdown() {
	c.cancel()
	close(c.updateChan)
	logger.Base().Info("AgentCache shutdown completed")
}

// ShutdownGlobal gracefully shuts down the global singleton instance
func ShutdownGlobal() {
	if instance != nil {
		instance.Shutdown()
		// Reset the singleton for potential restart
		instance = nil
		once = sync.Once{}
	}
}

// Ensure AgentCache implements the required interfaces
var _ config.AgentFetcher = (*AgentCache)(nil)
var _ config.AgentRepository = (*AgentCache)(nil)
