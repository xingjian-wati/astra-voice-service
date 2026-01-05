package event

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// LifecyclePhase represents the current phase of a connection
type LifecyclePhase int

const (
	PhaseCreated LifecyclePhase = iota
	PhaseInitializing
	PhaseReady
	PhaseTerminating
	PhaseTerminated
)

// String returns the string representation of the lifecycle phase
func (p LifecyclePhase) String() string {
	switch p {
	case PhaseCreated:
		return "created"
	case PhaseInitializing:
		return "initializing"
	case PhaseReady:
		return "ready"
	case PhaseTerminating:
		return "terminating"
	case PhaseTerminated:
		return "terminated"
	default:
		return "unknown"
	}
}

// ConnectionState represents the state of a connection in its lifecycle
type ConnectionState struct {
	ID              string                 `json:"id"`
	CallID          string                 `json:"call_id,omitempty"`
	TenantID        string                 `json:"tenant_id,omitempty"`
	Phase           LifecyclePhase         `json:"phase"`
	Dependencies    []string               `json:"dependencies"`
	ReadyConditions map[string]bool        `json:"ready_conditions"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// ConnectionLifecycle manages the lifecycle of connections
type ConnectionLifecycle struct {
	eventBus    EventBus
	connections map[string]*ConnectionState
	mutex       sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// Dependency keys for connection readiness
const (
	DepWebRTCAudioReady     = "webrtc_audio_ready"
	DepWebRTCSdpReady       = "webrtc_sdp_ready"
	DepAIConnectionReady    = "ai_connection_ready"
	DepAIAudioReady         = "ai_audio_ready"
	DepAIDataChannelReady   = "ai_data_channel_ready"
	DepWhatsAppAudioReady   = "whatsapp_audio_ready"
	DepWhatsAppCallAccepted = "whatsapp_call_accepted"
)

// NewConnectionLifecycle creates a new connection lifecycle manager
func NewConnectionLifecycle(eventBus EventBus) *ConnectionLifecycle {
	ctx, cancel := context.WithCancel(context.Background())

	lifecycle := &ConnectionLifecycle{
		eventBus:    eventBus,
		connections: make(map[string]*ConnectionState),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Subscribe to relevant events
	lifecycle.setupEventSubscriptions()

	return lifecycle
}

// RegisterConnection registers a new connection with its dependencies
func (cl *ConnectionLifecycle) RegisterConnection(connectionID, callID, tenantID string, dependencies []string) error {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()

	if _, exists := cl.connections[connectionID]; exists {
		return fmt.Errorf("connection %s already registered", connectionID)
	}

	// Initialize ready conditions based on dependencies
	readyConditions := make(map[string]bool)
	for _, dep := range dependencies {
		readyConditions[dep] = false
	}

	state := &ConnectionState{
		ID:              connectionID,
		CallID:          callID,
		TenantID:        tenantID,
		Phase:           PhaseCreated,
		Dependencies:    dependencies,
		ReadyConditions: readyConditions,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
		Metadata:        make(map[string]interface{}),
	}

	cl.connections[connectionID] = state

	logger.Base().Info("Registered connection", zap.String("connection_id", connectionID), zap.Strings("dependencies", dependencies))
	// Publish connection created event
	event := NewConnectionEvent(ConnectionCreated, connectionID).
		WithCallID(callID).
		WithTenantID(tenantID).
		WithData(&WhatsAppEventData{
			ConnectionID: connectionID,
			CallID:       callID,
		})

	return cl.eventBus.PublishEvent(event)
}

// UpdateConnectionPhase updates the phase of a connection
func (cl *ConnectionLifecycle) UpdateConnectionPhase(connectionID string, phase LifecyclePhase) error {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()

	state, exists := cl.connections[connectionID]
	if !exists {
		return fmt.Errorf("connection %s not found", connectionID)
	}

	oldPhase := state.Phase
	state.Phase = phase
	state.UpdatedAt = time.Now()

	logger.Base().Info("Connection phase changed", zap.Int("old_phase", int(oldPhase)), zap.String("connection_id", connectionID), zap.Int("new_phase", int(phase)))
	// Publish phase change event based on the new phase
	var eventType EventType
	switch phase {
	case PhaseReady:
		eventType = ConnectionReady
	case PhaseTerminated:
		eventType = ConnectionTerminated
	default:
		return nil // Don't publish events for intermediate phases
	}

	event := NewConnectionEvent(eventType, connectionID).
		WithCallID(state.CallID).
		WithTenantID(state.TenantID)

	return cl.eventBus.PublishEvent(event)
}

// MarkDependencyReady marks a dependency as ready and checks if connection is ready
func (cl *ConnectionLifecycle) MarkDependencyReady(connectionID, dependency string) error {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()

	state, exists := cl.connections[connectionID]
	if !exists {
		return fmt.Errorf("connection %s not found", connectionID)
	}

	if state.Phase >= PhaseReady {
		logger.Base().Info("Connection already ready, ignoring dependency", zap.String("connection_id", connectionID), zap.String("dependency", dependency))
		return nil
	}

	// Mark dependency as ready
	if _, exists := state.ReadyConditions[dependency]; exists {
		state.ReadyConditions[dependency] = true
		state.UpdatedAt = time.Now()

		logger.Base().Info("Dependency ready", zap.String("connection_id", connectionID), zap.String("dependency", dependency))
		// Check if all dependencies are ready
		if cl.areAllDependenciesReady(state) {
			logger.Base().Info("All dependencies ready for connection", zap.String("connection_id", connectionID))
			state.Phase = PhaseReady

			// Publish connection ready event
			event := NewConnectionEvent(ConnectionReady, connectionID).
				WithCallID(state.CallID).
				WithTenantID(state.TenantID)

			return cl.eventBus.PublishEvent(event)
		}
	} else {
		logger.Base().Warn("Unknown dependency for connection", zap.String("connection_id", connectionID), zap.String("dependency", dependency))
	}

	return nil
}

// GetConnectionState returns the current state of a connection
func (cl *ConnectionLifecycle) GetConnectionState(connectionID string) (*ConnectionState, error) {
	cl.mutex.RLock()
	defer cl.mutex.RUnlock()

	state, exists := cl.connections[connectionID]
	if !exists {
		return nil, fmt.Errorf("connection %s not found", connectionID)
	}

	// Return a copy to avoid race conditions
	stateCopy := *state
	stateCopy.ReadyConditions = make(map[string]bool)
	for k, v := range state.ReadyConditions {
		stateCopy.ReadyConditions[k] = v
	}
	stateCopy.Metadata = make(map[string]interface{})
	for k, v := range state.Metadata {
		stateCopy.Metadata[k] = v
	}

	return &stateCopy, nil
}

// TerminateConnection marks a connection as terminated and cleans up
func (cl *ConnectionLifecycle) TerminateConnection(connectionID string) error {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()

	state, exists := cl.connections[connectionID]
	if !exists {
		return fmt.Errorf("connection %s not found", connectionID)
	}

	if state.Phase == PhaseTerminated {
		logger.Base().Info("Connection already terminated", zap.String("connection_id", connectionID))
		return nil
	}

	state.Phase = PhaseTerminated
	state.UpdatedAt = time.Now()

	logger.Base().Info("Connection terminated", zap.String("connection_id", connectionID))
	// Publish termination event
	event := NewConnectionEvent(ConnectionTerminated, connectionID).
		WithCallID(state.CallID).
		WithTenantID(state.TenantID)

	if err := cl.eventBus.PublishEvent(event); err != nil {
		logger.Base().Error("Failed to publish termination event", zap.String("connection_id", connectionID), zap.Error(err))
	}

	// Clean up after a delay to allow event processing
	go func() {
		time.Sleep(5 * time.Second)
		cl.cleanupConnection(connectionID)
	}()

	return nil
}

// GetAllConnections returns all current connections
func (cl *ConnectionLifecycle) GetAllConnections() map[string]*ConnectionState {
	cl.mutex.RLock()
	defer cl.mutex.RUnlock()

	result := make(map[string]*ConnectionState)
	for id, state := range cl.connections {
		stateCopy := *state
		result[id] = &stateCopy
	}

	return result
}

// Close closes the lifecycle manager
func (cl *ConnectionLifecycle) Close() error {
	cl.cancel()

	cl.mutex.Lock()
	defer cl.mutex.Unlock()

	cl.connections = make(map[string]*ConnectionState)

	logger.Base().Info("🔒 Connection lifecycle manager closed")
	return nil
}

// setupEventSubscriptions sets up event subscriptions for lifecycle management
func (cl *ConnectionLifecycle) setupEventSubscriptions() {
	// Subscribe to WebRTC events
	cl.eventBus.Subscribe(AudioTrackReady, func(event *ConnectionEvent) {
		if data, ok := event.GetWebRTCData(); ok {
			cl.MarkDependencyReady(data.ConnectionID, DepWebRTCAudioReady)
		}
	})

	cl.eventBus.Subscribe(SDPAnswerGenerated, func(event *ConnectionEvent) {
		if data, ok := event.GetWebRTCData(); ok {
			cl.MarkDependencyReady(data.ConnectionID, DepWebRTCSdpReady)
		}
	})

	// Subscribe to AI/model events
	cl.eventBus.Subscribe(AIConnectionInit, func(event *ConnectionEvent) {
		if data, ok := event.GetAIData(); ok {
			cl.MarkDependencyReady(data.ConnectionID, DepAIConnectionReady)
		}
	})

	cl.eventBus.Subscribe(AIAudioReady, func(event *ConnectionEvent) {
		if data, ok := event.GetAIData(); ok {
			cl.MarkDependencyReady(data.ConnectionID, DepAIAudioReady)
		}
	})

	cl.eventBus.Subscribe(AIDataChannelReady, func(event *ConnectionEvent) {
		if data, ok := event.GetAIData(); ok {
			cl.MarkDependencyReady(data.ConnectionID, DepAIDataChannelReady)
		}
	})

	// Subscribe to WhatsApp events
	cl.eventBus.Subscribe(WhatsAppAudioReady, func(event *ConnectionEvent) {
		if data, ok := event.GetWhatsAppData(); ok {
			cl.MarkDependencyReady(data.ConnectionID, DepWhatsAppAudioReady)
		}
	})

	cl.eventBus.Subscribe(WhatsAppCallAccepted, func(event *ConnectionEvent) {
		if data, ok := event.GetWhatsAppData(); ok {
			cl.MarkDependencyReady(data.ConnectionID, DepWhatsAppCallAccepted)
		}
	})

	logger.Base().Info("Event subscriptions set up for lifecycle management")
}

// areAllDependenciesReady checks if all dependencies for a connection are ready
func (cl *ConnectionLifecycle) areAllDependenciesReady(state *ConnectionState) bool {
	for _, ready := range state.ReadyConditions {
		if !ready {
			return false
		}
	}
	return true
}

// cleanupConnection removes a connection from the lifecycle manager
func (cl *ConnectionLifecycle) cleanupConnection(connectionID string) {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()

	delete(cl.connections, connectionID)
	logger.Base().Info("Cleaned up connection from lifecycle", zap.String("connection_id", connectionID))
}
