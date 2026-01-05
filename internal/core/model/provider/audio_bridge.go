package provider

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/core/event"
	"github.com/ClareAI/astra-voice-service/internal/storage"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// HandleModelAudioTrack handles incoming audio from model provider and forwards it to channel connection.
func (h *BaseHandler) HandleModelAudioTrack(connectionID string, track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	providerName := "unknown"
	if h.Provider != nil {
		providerName = h.Provider.GetProviderType().String()
	}
	logger.Base().Info("Model audio track received",
		zap.String("provider", providerName),
		zap.String("connection_id", connectionID),
		zap.String("kind", track.Kind().String()),
		zap.String("codec", track.Codec().MimeType))

	// Get channel connection
	var connection CallConnection
	if h.ConnectionGetter != nil {
		connection = h.ConnectionGetter(connectionID)
	}
	if connection == nil {
		logger.Base().Error("Channel connection not found", zap.String("connection_id", connectionID))
		return
	}

	// Check if output track is available
	outputTrack := connection.GetWAOutputTrack()

	if outputTrack == nil {
		logger.Base().Info("Channel output track not ready yet, waiting for event", zap.String("connection_id", connectionID))

		if h.EventBusGetter != nil {
			eventBus := h.EventBusGetter()
			if eventBus != nil {
				err := eventBus.SubscribeWithTimeout(event.WhatsAppAudioReady, func(e *event.ConnectionEvent) {
					if e.ConnectionID == connectionID {
						logger.Base().Info("Channel audio ready event received", zap.String("connection_id", connectionID))
						outputTrack = connection.GetWAOutputTrack()
						if outputTrack != nil {
							h.ContinueAudioBridge(connectionID, track, connection, outputTrack)
						}
					}
				}, 5*time.Second)

				if err != nil {
					logger.Base().Error("Failed to subscribe to audio ready event")
				}
				return
			}
		}

		logger.Base().Error("No event bus available, cannot wait for audio", zap.String("connection_id", connectionID))
		return
	}

	h.ContinueAudioBridge(connectionID, track, connection, outputTrack)
}

// ContinueAudioBridge sets up the audio bridge between AI and channel.
func (h *BaseHandler) ContinueAudioBridge(connectionID string, track *webrtc.TrackRemote, connection CallConnection, outputTrack OpusWriter) {
	logger.Base().Info("Starting AI audio bridge", zap.String("connection_id", connectionID))

	if outputTrack == nil {
		logger.Base().Error("Cannot start audio bridge: outputTrack is nil", zap.String("connection_id", connectionID))
		return
	}

	// Mark model as ready and trigger initial greeting
	connection.SetAIReady(true)
	if !connection.TryMarkGreetingSent() {
		logger.Base().Warn("Greeting already scheduled/sent, skipping duplicate", zap.String("connection_id", connectionID))
		return
	}
	go h.WaitAndSendGreeting(connectionID, connection)

	audioCache := storage.GetAudioCache()
	needsCaching := connection.NeedsAudioCaching()

	loadedBGMFrames, _ := LoadBGMFrames(strings.TrimSpace(DefaultBGMPath))

	opts := AudioBridgeOptions{
		ConnectionID:  connectionID,
		Track:         track,
		Output:        outputTrack,
		Connection:    connection.(AudioBridgeConnection),
		AudioCache:    audioCache,
		NeedsCaching:  needsCaching,
		SilenceFilter: DefaultSilenceFilter,
		MarkAudioActivity: func(id string) {
			h.MarkAudioActivity(id)
		},
		IsFunctionCallActive: func(id string) bool {
			return h.IsFunctionCallActive(id)
		},
		BGMFrames:           loadedBGMFrames,
		BGMSilenceThreshold: DefaultBGMSilenceThreshold,
		OnFirstPacket: func() {
			connection.SetGreetingAudioStartTime(time.Now())
			logger.Base().Info("🔊 Model audio started flowing", zap.String("connection_id", connectionID))
		},
		OnStop: func(packetCount int64) {
			if audioCache != nil && needsCaching {
				audioCache.CleanupConnection(connectionID)
			}
			logger.Base().Info("Audio bridge stopped", zap.Int64("packet_count", packetCount), zap.String("connection_id", connectionID))
		},
		LoggerPrefix: "model",
	}

	StartModelAudioForwarding(context.Background(), opts)
}

// WaitAndSendGreeting waits for data channel to be ready and sends greeting.
func (h *BaseHandler) WaitAndSendGreeting(connectionID string, connection CallConnection) {
	signalChan, hasSignalControl := h.GetGreetingSignalChan(connectionID)

	if hasSignalControl {
		logger.Base().Info("Waiting for greeting signal...", zap.String("connection_id", connectionID))
		signalReceived := false
		for !signalReceived {
			select {
			case <-signalChan:
				logger.Base().Info("Greeting signal received", zap.String("connection_id", connectionID))
				signalReceived = true
			case <-time.After(1 * time.Second):
				if connection.IsClosed() {
					return
				}
			}
		}
	}

	// Quick check
	if conn, exists := h.GetConnection(connectionID); exists && conn != nil && conn.IsConnected() {
		h.SendGreetingOnce(connectionID, connection)
		return
	}

	// Backoff retry
	intervals := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond, 500 * time.Millisecond}
	start := time.Now()
	timeout := 5 * time.Second

	for _, interval := range intervals {
		if time.Since(start) >= timeout {
			break
		}
		time.Sleep(interval)
		if conn, exists := h.GetConnection(connectionID); exists && conn != nil && conn.IsConnected() {
			h.SendGreetingOnce(connectionID, connection)
			return
		}
	}

	// Polling fallback
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(timeout - time.Since(start))

	for {
		select {
		case <-ticker.C:
			if conn, exists := h.GetConnection(connectionID); exists && conn != nil && conn.IsConnected() {
				h.SendGreetingOnce(connectionID, connection)
				return
			}
		case <-deadline:
			logger.Base().Info("Data channel timeout", zap.String("connection_id", connectionID))
			connection.SetGreetingSent(false)
			return
		}
	}
}

// SendGreetingOnce handles common greeting flag checks and rollback.
func (h *BaseHandler) SendGreetingOnce(connectionID string, connection CallConnection) {
	if !connection.IsGreetingSent() {
		if !connection.TryMarkGreetingSent() {
			return
		}
	}

	if h.OnSendInitialGreeting != nil {
		if err := h.OnSendInitialGreeting(connectionID); err != nil {
			logger.Base().Error("Failed to send initial greeting", zap.Error(err))
			connection.SetGreetingSent(false)
			return
		}
	}
	logger.Base().Info("Initial greeting triggered successfully", zap.String("connection_id", connectionID))
}

// AudioBridgeConnection defines the minimal surface needed by the audio bridge.
type AudioBridgeConnection interface {
	IsClosed() bool
	NeedsAudioCaching() bool
	SetAIReady(bool)
	TryMarkGreetingSent() bool
	SetGreetingAudioStartTime(time.Time)
	SetGreetingSent(bool)
	UpdateLastActivity()
}

// OpusWriter is the sink for writing Opus frames (e.g., WhatsApp output).
type OpusWriter interface {
	WriteOpusFrame(opusPayload []byte) error
}

// AudioBridgeOptions configures the model->WA audio forwarding bridge.
type AudioBridgeOptions struct {
	ConnectionID string
	Track        *webrtc.TrackRemote
	Output       OpusWriter
	Connection   AudioBridgeConnection

	AudioCache   *storage.AudioCacheService
	NeedsCaching bool

	SilenceFilter        func([]byte) bool
	MarkAudioActivity    func(string)
	IsFunctionCallActive func(string) bool
	BGMFrames            [][]byte
	BGMSilenceThreshold  time.Duration

	OnFirstPacket func()
	OnStop        func(packetCount int64)

	LoggerPrefix string
}

// DefaultSilenceFilter is a heuristic to drop typical Opus DTX/CN or empty payloads.
func DefaultSilenceFilter(p []byte) bool {
	if len(p) == 1 && (p[0] == 0xF8 || p[0] == 0x48) { // common Opus DTX/CN
		return true
	}
	if len(p) <= 3 {
		allZero := true
		for _, b := range p {
			if b != 0 {
				allZero = false
				break
			}
		}
		return allZero
	}
	return false
}

// StartModelAudioForwarding starts forwarding model RTP (Opus) to an Opus writer.
// It returns immediately and runs in a goroutine.
func StartModelAudioForwarding(ctx context.Context, opts AudioBridgeOptions) {
	// Safety checks
	if opts.Track == nil || opts.Output == nil || opts.Connection == nil {
		logger.Base().Error("audio bridge missing required components",
			zap.String("connection_id", opts.ConnectionID))
		return
	}

	prefix := opts.LoggerPrefix
	if prefix == "" {
		prefix = "model"
	}

	go func() {
		silenceFilter := opts.SilenceFilter
		if silenceFilter == nil {
			silenceFilter = DefaultSilenceFilter
		}

		// Shared last audio activity timestamp (nanoseconds)
		var lastAudioNano int64 = time.Now().UnixNano()
		// BGM goroutine (optional)
		stopBGM := make(chan struct{})
		if len(opts.BGMFrames) > 0 && opts.IsFunctionCallActive != nil {
			go func() {
				ticker := time.NewTicker(20 * time.Millisecond)
				defer ticker.Stop()
				var bgmIndex int64
				for {
					select {
					case <-stopBGM:
						return
					case <-ctx.Done():
						return
					case <-ticker.C:
						last := time.Unix(0, atomic.LoadInt64(&lastAudioNano))
						threshold := opts.BGMSilenceThreshold
						if threshold == 0 {
							threshold = 1 * time.Second
						}
						if time.Since(last) < threshold {
							continue
						}
						if !opts.IsFunctionCallActive(opts.ConnectionID) {
							continue
						}
						if len(opts.BGMFrames) == 0 {
							continue
						}
						idx := atomic.AddInt64(&bgmIndex, 1) - 1
						frame := opts.BGMFrames[int(idx)%len(opts.BGMFrames)]
						if err := opts.Output.WriteOpusFrame(frame); err != nil {
							logger.Base().Error("Failed to write BGM",
								zap.String("prefix", prefix),
								zap.String("connection_id", opts.ConnectionID),
								zap.Error(err))
						} else {
							atomic.StoreInt64(&lastAudioNano, time.Now().UnixNano())
						}
					}
				}
			}()
		}

		var packetCount int64
		defer func() {
			close(stopBGM)
			logger.Base().Info("Audio bridge stopped",
				zap.String("prefix", prefix),
				zap.Int64("packet_count", packetCount),
				zap.String("connection_id", opts.ConnectionID))
			if opts.OnStop != nil {
				opts.OnStop(packetCount)
			}
		}()

		writeErrorCount := 0
		lastFrame := []byte(nil)
		repeatFrameCount := 0

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			rtpPacket, _, err := opts.Track.ReadRTP()
			if err != nil {
				logger.Base().Debug("Model audio track ended",
					zap.String("prefix", prefix),
					zap.Int64("packet_count", packetCount),
					zap.String("connection_id", opts.ConnectionID))
				return
			}
			packetCount++

			// First packet callback
			if packetCount == 1 && opts.OnFirstPacket != nil {
				opts.OnFirstPacket()
			}

			// Connection closed?
			if opts.Connection.IsClosed() {
				logger.Base().Info("Connection closed, stopping model audio forwarding",
					zap.String("connection_id", opts.ConnectionID))
				return
			}

			payload := rtpPacket.Payload
			if len(payload) == 0 {
				continue
			}

			// Cache audio if needed
			if opts.AudioCache != nil && opts.NeedsCaching {
				opts.AudioCache.CacheAudioRTP(opts.ConnectionID, storage.AudioTypeAIOutput, storage.AudioFormatOpus, rtpPacket)
			}

			// Silence / repeat filtering
			if silenceFilter != nil && silenceFilter(payload) {
				continue
			}
			if len(lastFrame) == len(payload) {
				same := true
				for i := range payload {
					if payload[i] != lastFrame[i] {
						same = false
						break
					}
				}
				if same {
					repeatFrameCount++
					if repeatFrameCount >= 3 {
						continue
					}
				} else {
					lastFrame = append(lastFrame[:0], payload...)
					repeatFrameCount = 0
				}
			} else {
				lastFrame = append(lastFrame[:0], payload...)
				repeatFrameCount = 0
			}

			// Mark audio activity (for VAD/metrics) if provided
			if opts.MarkAudioActivity != nil {
				opts.MarkAudioActivity(opts.ConnectionID)
			}

			// Write to WA
			if err := opts.Output.WriteOpusFrame(payload); err != nil {
				if writeErrorCount < 3 {
					logger.Base().Error("Failed to write audio",
						zap.String("prefix", prefix),
						zap.String("connection_id", opts.ConnectionID),
						zap.Error(err))
				}
				writeErrorCount++
				continue
			}

			opts.Connection.UpdateLastActivity()
			atomic.StoreInt64(&lastAudioNano, time.Now().UnixNano())
		}
	}()
}
