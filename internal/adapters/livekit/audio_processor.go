package livekit

import (
	"sync/atomic"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/services/call"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
	"layeh.com/gopus"
)

// AudioProcessor handles audio encoding/decoding for LiveKit connections
type AudioProcessor struct {
	// No shared state anymore
}

// NewAudioProcessor creates a new audio processor with Opus codec support
func NewAudioProcessor() (*AudioProcessor, error) {
	return &AudioProcessor{}, nil
}

// ForwardLiveKitAudioToAI processes audio from LiveKit client and forwards to the model
func (ap *AudioProcessor) ForwardLiveKitAudioToAI(connectionID string, track *webrtc.TrackRemote, connection *call.WhatsAppCallConnection) {
	logger.Base().Info("🎧 Starting audio processing: LiveKit → AI (connection: )", zap.String("connection_id", connectionID))

	defer func() {
		logger.Base().Info("🛑 Audio processing stopped for", zap.String("connection_id", connectionID))
	}()

	var frameCount int64
	var suppressionCount int64
	var dtxFrameCount int64
	var consecutiveDTXCount int64 // 连续 DTX 帧计数

	logger.Base().Info("Audio forwarding ready for: (DTX frames will be handled)", zap.String("connection_id", connectionID))

	for {
		// Read RTP packet from LiveKit track
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			logger.Base().Debug("Audio track ended for", zap.String("connection_id", connectionID))
			return
		}

		// Check atomically if connection is still active
		if atomic.LoadInt32(&connection.AtomicClosed) == 1 {
			logger.Base().Info("🛑 Connection closed, stopping LiveKit audio processing", zap.String("connection_id", connectionID))
			return
		}

		// Extract Opus payload from RTP packet
		opusPayload := rtpPacket.Payload
		if len(opusPayload) == 0 {
			continue
		}

		// Decode Opus to PCM16 for AI
		// Use connection-specific decoder
		connection.Mutex.RLock()
		decoder := connection.OpusDecoder
		connection.Mutex.RUnlock()

		if decoder == nil {
			// Initialize decoder if not present (thread-safe initialization)
			connection.Mutex.Lock()
			// Double-check locking
			if connection.OpusDecoder == nil {
				var err error
				// Create Opus decoder for 48kHz, 1 channel (mono)
				connection.OpusDecoder, err = gopus.NewDecoder(48000, 1)
				if err != nil {
					logger.Base().Error("Failed to create Opus decoder")
					connection.Mutex.Unlock()
					return
				}
			}
			decoder = connection.OpusDecoder
			connection.Mutex.Unlock()
		}

		var pcmSamples []int16
		var decodeErr error

		// 🔥 智能处理 DTX/静音帧（< 3 字节）
		// ✅ 优化策略：稀疏发送 DTX 静音帧，减少对模型 VAD 的干扰
		if len(opusPayload) < 3 {
			dtxFrameCount++
			consecutiveDTXCount++

			// 🎯 关键优化：每 4 个连续 DTX 帧只发送 1 个
			// 这样可以让模型 VAD 更快识别到连续静音，而不是断续的静音信号
			if consecutiveDTXCount%4 == 1 {
				// 只发送每组的第一个 DTX 帧
				pcmSamples = make([]int16, 960) // 20ms @ 48kHz，全零静音
			} else {
				// 跳过其他 DTX 帧，不发送给模型
				continue
			}
		} else {
			// 重置连续 DTX 计数（遇到正常音频帧）
			consecutiveDTXCount = 0

			// Decode normal Opus frames
			// Use larger buffer to handle variable frame sizes (same as WhatsApp)
			// Opus can use 20ms, 40ms, or 60ms frames - need larger buffer for safety
			maxSamples := 1920 // 40ms at 48kHz mono (double buffer for safety)
			pcmSamples, decodeErr = decoder.Decode(opusPayload, maxSamples, false)
			if decodeErr != nil {
				// Log decode failures more verbosely to debug latency issues
				logger.Base().Error("Decode failed", zap.Uint16("sequence_number", rtpPacket.SequenceNumber), zap.Int("size_bytes", len(opusPayload)), zap.Error(decodeErr))
				continue
			}
		}

		// Send PCM16 samples to the model (fast path - highest priority)
		if len(pcmSamples) > 0 && connection.AIWebRTC != nil {
			// Check if we should forward audio to the model
			// If greeting hasn't been sent/completed yet, and connection is very new,
			// suppress user audio to prevent interrupting the greeting
			shouldForward, reason := connection.ShouldForwardAudioToAI()

			if !shouldForward {
				suppressionCount++
				// Log suppression occasionally (every 100 suppressed frames)
				if suppressionCount%100 == 0 {
					logger.Base().Info("🤫 Suppressing user audio during greeting phase ()", zap.String("reason", reason), zap.String("connection_id", connectionID))
				}
				// Still update activity
				connection.Mutex.Lock()
				connection.LastActivity = time.Now()
				connection.Mutex.Unlock()
				continue
			}

			// Send immediately to the model
			if err := connection.AIWebRTC.SendAudio(pcmSamples); err == nil {
				frameCount++

				// 定期打印音频流状态和 DTX 统计（降低频率减少日志）
				if frameCount%200 == 0 {
					totalFrames := frameCount + dtxFrameCount
					if dtxFrameCount > 0 {
						dtxPercentage := float64(dtxFrameCount) / float64(totalFrames) * 100
						// 计算实际发送的 DTX 静音帧数（每 4 个只发 1 个）
						sentDTXFrames := (dtxFrameCount + 3) / 4
						logger.Base().Info("Audio flowing with DTX", zap.Int64("sent_dtx_frames", sentDTXFrames), zap.Int64("detected_dtx_frames", dtxFrameCount), zap.Int64("frame_count", frameCount), zap.Float64("dtx_percentage", dtxPercentage))
					} else {
						logger.Base().Info("Audio flowing", zap.Int64("frame_count", frameCount))
					}
					// 不重置 dtxFrameCount，累计统计
				}
			} else {
				// Log send failures
				logger.Base().Error("SendAudio failed")
			}
		} else if len(pcmSamples) == 0 {
			// Log zero-sample decodes (shouldn't happen often)
			if rtpPacket.SequenceNumber%100 == 0 {
				logger.Base().Warn("Opus decode returned 0 samples", zap.Int("bytes", len(opusPayload)))
			}
		}

		// Update connection activity (with lock protection to prevent concurrent map writes or data races)
		connection.Mutex.Lock()
		connection.LastActivity = time.Now()
		connection.Mutex.Unlock()
	}
}

// Note: ForwardAIAudioToLiveKit is no longer needed
// Model audio is handled through LiveKitOpusWriter (opus_writer.go)
// which implements the PionOpusWriter interface and is set as WAOutputTrack

// Close cleans up audio processor resources
func (ap *AudioProcessor) Close() {
	logger.Base().Info("🧹 Audio processor cleaned up")
	// Opus encoder/decoder don't need explicit cleanup in gopus
}
