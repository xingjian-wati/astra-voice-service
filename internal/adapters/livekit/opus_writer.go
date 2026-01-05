package livekit

import (
	"time"

	lksdk "github.com/livekit/server-sdk-go"
	"github.com/pion/webrtc/v3/pkg/media"
)

// LiveKitOpusWriter implements PionOpusWriter interface for LiveKit
// This allows ai's audio handler to write directly to LiveKit track
type LiveKitOpusWriter struct {
	track        *lksdk.LocalSampleTrack
	connectionID string
	frameCount   int64
}

// NewLiveKitOpusWriter creates a new LiveKit Opus writer
func NewLiveKitOpusWriter(track *lksdk.LocalSampleTrack, connectionID string) *LiveKitOpusWriter {
	return &LiveKitOpusWriter{
		track:        track,
		connectionID: connectionID,
		frameCount:   0,
	}
}

// WriteOpusFrame writes an Opus frame to the LiveKit track
// This implements the PionOpusWriter interface used by ai handler
func (w *LiveKitOpusWriter) WriteOpusFrame(opusPayload []byte) error {
	if w.track == nil {
		return nil // Gracefully handle nil track
	}

	// Create media sample for LiveKit with strict 20ms framing
	// Duration MUST match actual Opus frame size to avoid buffering/drift
	sample := media.Sample{
		Data:     opusPayload,
		Duration: 20 * time.Millisecond, // Strict 20ms for low latency
	}

	// Write to LiveKit track (highest priority)
	if err := w.track.WriteSample(sample, nil); err != nil {
		return err
	}

	w.frameCount++

	return nil
}
