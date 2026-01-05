package event

import (
	"fmt"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// LoggingMiddleware provides logging for all events
func LoggingMiddleware(next EventHandler) EventHandler {
	return func(event *ConnectionEvent) {
		start := time.Now()

		logger.Base().Info("Processing event", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID))
		defer func() {
			duration := time.Since(start)
			if event.IsError() {
				logger.Base().Error("Event handler failed", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID), zap.Error(event.Error))
			} else {
				logger.Base().Info("Event handler completed", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID), zap.Duration("duration", duration))
			}
		}()

		next(event)
	}
}

// MetricsMiddleware provides metrics collection for events
func MetricsMiddleware(next EventHandler) EventHandler {
	return func(event *ConnectionEvent) {
		start := time.Now()

		defer func() {
			duration := time.Since(start)

			// Here you would typically send metrics to your monitoring system
			// For now, we'll just log the metrics
			logger.Base().Info("Event metrics", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID), zap.Duration("duration", duration))
			if r := recover(); r != nil {
				logger.Base().Error("Event handler panic", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID), zap.Any("panic", r))
				panic(r) // Re-panic to maintain the panic behavior
			}
		}()

		next(event)
	}
}

// RecoveryMiddleware provides panic recovery for event handlers
func RecoveryMiddleware(next EventHandler) EventHandler {
	return func(event *ConnectionEvent) {
		defer func() {
			if r := recover(); r != nil {
				logger.Base().Error("Panic in event handler", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID), zap.Any("panic", r))
				// You might want to publish an error event here
				errorEvent := NewConnectionEvent(HandlerPanic, event.ConnectionID).
					WithError(fmt.Errorf("handler panic: %v", r)).
					WithData(map[string]interface{}{
						"original_event_type": event.Type,
						"panic_value":         r,
					})

				// Note: Be careful not to cause infinite recursion here
				logger.Base().Error("Publishing error event for panic", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID))
				_ = errorEvent // In a real implementation, you'd publish this to a dead letter queue
			}
		}()

		next(event)
	}
}

// TimeoutMiddleware provides timeout functionality for event handlers
func TimeoutMiddleware(timeout time.Duration) EventMiddleware {
	return func(next EventHandler) EventHandler {
		return func(event *ConnectionEvent) {
			done := make(chan struct{})

			go func() {
				defer close(done)
				next(event)
			}()

			select {
			case <-done:
				// Handler completed successfully
			case <-time.After(timeout):
				logger.Base().Info("Event handler timeout", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID), zap.Duration("timeout", timeout))
			}
		}
	}
}

// ValidationMiddleware validates events before processing
func ValidationMiddleware(next EventHandler) EventHandler {
	return func(event *ConnectionEvent) {
		// Basic validation
		if event == nil {
			logger.Base().Error("Received nil event")
			return
		}

		if event.Type == "" {
			logger.Base().Error("Event type is empty", zap.String("connection_id", event.ConnectionID))
			return
		}

		if event.ConnectionID == "" {
			logger.Base().Error("Connection ID is empty", zap.String("type", string(event.Type)))
			return
		}

		// Validate event-specific data
		if err := validateEventData(event); err != nil {
			logger.Base().Error("Invalid event data", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID), zap.Error(err))
			return
		}

		next(event)
	}
}

// RateLimitMiddleware provides rate limiting for events
func RateLimitMiddleware(maxEventsPerSecond int) EventMiddleware {
	ticker := time.NewTicker(time.Second / time.Duration(maxEventsPerSecond))

	return func(next EventHandler) EventHandler {
		return func(event *ConnectionEvent) {
			select {
			case <-ticker.C:
				next(event)
			default:
				logger.Base().Info("Event dropped due to rate limiting", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID))
			}
		}
	}
}

// DeduplicationMiddleware prevents duplicate events within a time window
func DeduplicationMiddleware(windowSize time.Duration) EventMiddleware {
	eventCache := make(map[string]time.Time)

	return func(next EventHandler) EventHandler {
		return func(event *ConnectionEvent) {
			key := fmt.Sprintf("%s:%s", event.Type, event.ConnectionID)

			if lastSeen, exists := eventCache[key]; exists {
				if time.Since(lastSeen) < windowSize {
					logger.Base().Info("Duplicate event within window", zap.String("type", string(event.Type)), zap.String("connection_id", event.ConnectionID), zap.Duration("window_size", windowSize))
					return
				}
			}

			eventCache[key] = time.Now()

			// Clean up old entries periodically
			go func() {
				time.Sleep(windowSize * 2)
				if lastSeen, exists := eventCache[key]; exists && time.Since(lastSeen) > windowSize {
					delete(eventCache, key)
				}
			}()

			next(event)
		}
	}
}

// validateEventData validates event-specific data
func validateEventData(event *ConnectionEvent) error {
	switch event.Type {
	case SDPOfferReceived, SDPAnswerGenerated:
		if data, ok := event.GetWebRTCData(); ok {
			if data.SDP == "" {
				return fmt.Errorf("SDP data is required for %s", event.Type)
			}
		} else {
			return fmt.Errorf("WebRTC data is required for %s", event.Type)
		}

	case AudioTrackReady:
		if data, ok := event.GetWebRTCData(); ok {
			if data.TrackType == "" {
				return fmt.Errorf("track type is required for %s", event.Type)
			}
		} else {
			return fmt.Errorf("WebRTC data is required for %s", event.Type)
		}

	case WhatsAppCallStarted, WhatsAppCallAccepted:
		if data, ok := event.GetWhatsAppData(); ok {
			if data.CallID == "" {
				return fmt.Errorf("call ID is required for %s", event.Type)
			}
		} else {
			return fmt.Errorf("WhatsApp data is required for %s", event.Type)
		}
	}

	return nil
}

// CreateDefaultMiddlewareChain creates a default middleware chain with common middleware
func CreateDefaultMiddlewareChain() []EventMiddleware {
	return []EventMiddleware{
		RecoveryMiddleware,
		ValidationMiddleware,
		LoggingMiddleware,
		MetricsMiddleware,
		DeduplicationMiddleware(5 * time.Second),
	}
}

// CreateProductionMiddlewareChain creates a production-ready middleware chain
func CreateProductionMiddlewareChain() []EventMiddleware {
	return []EventMiddleware{
		RecoveryMiddleware,
		ValidationMiddleware,
		TimeoutMiddleware(30 * time.Second),
		RateLimitMiddleware(100), // 100 events per second max
		DeduplicationMiddleware(5 * time.Second),
		MetricsMiddleware,
		LoggingMiddleware,
	}
}
