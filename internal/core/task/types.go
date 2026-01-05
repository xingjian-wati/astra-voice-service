package task

import (
	"context"
)

// TaskType defines the type of asynchronous task
type TaskType string

const (
	TaskTypeInboundCall  TaskType = "inbound_call"  // Process Wati inbound call (SDP & AI Init)
	TaskTypeWebCall      TaskType = "web_call"      // Process Web/Test inbound call
	TaskTypeOutboundCall TaskType = "outbound_call" // Process Outbound call setup
	TaskTypeLiveKitRoom  TaskType = "livekit_room"  // Process LiveKit room setup & AI Init
)

// SessionTask represents an asynchronous task payload
type SessionTask struct {
	Type         TaskType `json:"type"`
	ConnectionID string   `json:"connection_id"`
	Payload      []byte   `json:"payload"` // JSON payload of the original request
}

// Bus defines the interface for the task bus
type Bus interface {
	Publish(ctx context.Context, task SessionTask) error
	Subscribe(ctx context.Context, handler func(SessionTask)) error
}
