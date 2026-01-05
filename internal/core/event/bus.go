package event

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// EventHandler represents a function that handles events
type EventHandler func(event *ConnectionEvent)

// EventMiddleware represents middleware that can wrap event handlers
type EventMiddleware func(next EventHandler) EventHandler

// EventBus defines the interface for event bus operations
type EventBus interface {
	Publish(eventType EventType, data interface{}) error
	PublishEvent(event *ConnectionEvent) error
	Subscribe(eventType EventType, handler EventHandler) error
	SubscribeWithTimeout(eventType EventType, handler EventHandler, timeout time.Duration) error
	Unsubscribe(eventType EventType, handler EventHandler) error
	Use(middleware EventMiddleware)
	Close() error
	GetStats() BusStats
}

// BusStats contains statistics about the event bus
type BusStats struct {
	TotalEvents     int64            `json:"total_events"`
	EventsByType    map[string]int64 `json:"events_by_type"`
	ActiveHandlers  int              `json:"active_handlers"`
	SubscriberCount map[string]int   `json:"subscriber_count"`
}

// DefaultEventBus is the default implementation of EventBus
type DefaultEventBus struct {
	subscribers map[EventType][]EventHandler
	middleware  []EventMiddleware
	mutex       sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	stats       BusStats
	statsMutex  sync.RWMutex
}

// NewEventBus creates a new event bus instance
func NewEventBus() EventBus {
	ctx, cancel := context.WithCancel(context.Background())

	return &DefaultEventBus{
		subscribers: make(map[EventType][]EventHandler),
		middleware:  make([]EventMiddleware, 0),
		ctx:         ctx,
		cancel:      cancel,
		stats: BusStats{
			EventsByType:    make(map[string]int64),
			SubscriberCount: make(map[string]int),
		},
	}
}

// Publish publishes an event with the given type and data
func (b *DefaultEventBus) Publish(eventType EventType, data interface{}) error {
	event := NewConnectionEvent(eventType, "")
	if data != nil {
		event.Data = data

		// Extract connection ID from data if available
		switch d := data.(type) {
		case *WebRTCEventData:
			event.ConnectionID = d.ConnectionID
		case *AIEventData:
			event.ConnectionID = d.ConnectionID
		case *WhatsAppEventData:
			event.ConnectionID = d.ConnectionID
		}
	}

	return b.PublishEvent(event)
}

// PublishEvent publishes a complete event
func (b *DefaultEventBus) PublishEvent(event *ConnectionEvent) error {
	select {
	case <-b.ctx.Done():
		return fmt.Errorf("event bus is closed")
	default:
	}

	b.mutex.RLock()
	handlers, exists := b.subscribers[event.Type]
	if !exists {
		b.mutex.RUnlock()
		logger.Base().Info("No subscribers for event type", zap.String("type", string(event.Type)))
		return nil
	}

	// Create a copy of handlers to avoid holding the lock during execution
	handlersCopy := make([]EventHandler, len(handlers))
	copy(handlersCopy, handlers)
	b.mutex.RUnlock()

	// Update statistics
	b.updateStats(event.Type)

	logger.Base().Info("Publishing event", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID), zap.Any("subscribers", b.subscribers))

	// Execute handlers asynchronously
	for _, handler := range handlersCopy {
		go func(h EventHandler) {
			defer func() {
				if r := recover(); r != nil {
					logger.Base().Error("Event handler panic", zap.String("type", string(event.Type)), zap.Any("panic", r))
				}
			}()

			// Apply middleware chain
			finalHandler := h
			for i := len(b.middleware) - 1; i >= 0; i-- {
				finalHandler = b.middleware[i](finalHandler)
			}

			finalHandler(event)
		}(handler)
	}

	return nil
}

// Subscribe subscribes to events of a specific type
func (b *DefaultEventBus) Subscribe(eventType EventType, handler EventHandler) error {
	return b.SubscribeWithTimeout(eventType, handler, 0)
}

// SubscribeWithTimeout subscribes to events with a timeout
func (b *DefaultEventBus) SubscribeWithTimeout(eventType EventType, handler EventHandler, timeout time.Duration) error {
	select {
	case <-b.ctx.Done():
		return fmt.Errorf("event bus is closed")
	default:
	}

	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Wrap handler with timeout if specified
	finalHandler := handler
	if timeout > 0 {
		finalHandler = b.withTimeout(handler, timeout)
	}

	b.subscribers[eventType] = append(b.subscribers[eventType], finalHandler)

	// Update subscriber count
	b.statsMutex.Lock()
	b.stats.SubscriberCount[string(eventType)]++
	b.stats.ActiveHandlers++
	b.statsMutex.Unlock()

	logger.Base().Info("Subscribed to event type", zap.String("event_type", string(eventType)), zap.Any("subscribers", b.subscribers))

	return nil
}

// Unsubscribe removes a handler from event subscriptions
func (b *DefaultEventBus) Unsubscribe(eventType EventType, handler EventHandler) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	handlers, exists := b.subscribers[eventType]
	if !exists {
		return fmt.Errorf("no subscribers for event type: %s", eventType)
	}

	// Note: This is a simplified implementation
	// In a production system, you might want to use handler IDs for precise removal
	for i, h := range handlers {
		if fmt.Sprintf("%p", h) == fmt.Sprintf("%p", handler) {
			b.subscribers[eventType] = append(handlers[:i], handlers[i+1:]...)

			// Update subscriber count
			b.statsMutex.Lock()
			b.stats.SubscriberCount[string(eventType)]--
			b.stats.ActiveHandlers--
			b.statsMutex.Unlock()

			logger.Base().Info("Unsubscribed from event type", zap.String("event_type", string(eventType)))
			return nil
		}
	}

	return fmt.Errorf("handler not found for event type: %s", eventType)
}

// Use adds middleware to the event bus
func (b *DefaultEventBus) Use(middleware EventMiddleware) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.middleware = append(b.middleware, middleware)
}

// Close closes the event bus and cancels all operations
func (b *DefaultEventBus) Close() error {
	b.cancel()

	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Clear all subscribers
	b.subscribers = make(map[EventType][]EventHandler)
	b.middleware = make([]EventMiddleware, 0)

	logger.Base().Info("🔒 Event bus closed")
	return nil
}

// GetStats returns current bus statistics
func (b *DefaultEventBus) GetStats() BusStats {
	b.statsMutex.RLock()
	defer b.statsMutex.RUnlock()

	// Create a copy to avoid race conditions
	stats := BusStats{
		TotalEvents:     b.stats.TotalEvents,
		EventsByType:    make(map[string]int64),
		ActiveHandlers:  b.stats.ActiveHandlers,
		SubscriberCount: make(map[string]int),
	}

	for k, v := range b.stats.EventsByType {
		stats.EventsByType[k] = v
	}

	for k, v := range b.stats.SubscriberCount {
		stats.SubscriberCount[k] = v
	}

	return stats
}

// withTimeout wraps a handler with timeout functionality
func (b *DefaultEventBus) withTimeout(handler EventHandler, timeout time.Duration) EventHandler {
	return func(event *ConnectionEvent) {
		done := make(chan struct{})

		go func() {
			defer close(done)
			handler(event)
		}()

		select {
		case <-done:
			// Handler completed successfully
		case <-time.After(timeout):
			logger.Base().Info("Event handler timeout", zap.String("type", string(event.Type)), zap.Duration("timeout", timeout))
		case <-b.ctx.Done():
			logger.Base().Info("Event handler cancelled", zap.String("type", string(event.Type)))
		}
	}
}

// updateStats updates event statistics
func (b *DefaultEventBus) updateStats(eventType EventType) {
	b.statsMutex.Lock()
	defer b.statsMutex.Unlock()

	b.stats.TotalEvents++
	b.stats.EventsByType[string(eventType)]++
}
