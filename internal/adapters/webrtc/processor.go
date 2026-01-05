package webrtc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/event"
	"github.com/ClareAI/astra-voice-service/internal/storage"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
	"layeh.com/gopus"
)

// Processor handles WebRTC connections for WhatsApp calls using Pion WriteSample
type Processor struct {
	service ServiceInterface
	mutex   sync.RWMutex

	// WebRTC peer connections
	peerConnections map[string]*webrtc.PeerConnection

	// Pending outbound connections (waiting for SDP answer)
	pendingOutbound map[string]*pendingOutboundConn

	// Note: Opus decoder is now per-connection (in *call.WhatsAppCallConnection struct)
	// to avoid concurrent access issues and SIGSEGV crashes
}

// pendingOutboundConn holds state for outbound call waiting for answer
type pendingOutboundConn struct {
	pc          *webrtc.PeerConnection
	outputTrack *webrtc.TrackLocalStaticSample
	sender      *webrtc.RTPSender
}

// NewProcessor creates a new WebRTC processor for WhatsApp calls
func NewProcessor(service ServiceInterface) *Processor {
	logger.Base().Info("Creating WebRTC Processor (Opus decoders will be created per-connection)")

	return &Processor{
		service:         service,
		peerConnections: make(map[string]*webrtc.PeerConnection),
		pendingOutbound: make(map[string]*pendingOutboundConn),
		// Note: No global decoder - each connection will have its own
	}
}

// ProcessSDPOffer processes an SDP offer using the expert-recommended Pion template
func (p *Processor) ProcessSDPOffer(connectionID, offerSDP string) (string, error) {
	logger.Base().Info("Processing SDP offer", zap.String("connection_id", connectionID))
	// Get STUN servers from configuration
	stunServers := p.service.GetSTUNServers()
	if len(stunServers) == 0 {
		// Fallback to default if not configured
		stunServers = []string{
			config.DefaultSTUNServer1,
			config.DefaultSTUNServer2,
		}
		logger.Base().Warn("No STUN servers configured, using defaults")
	}

	// Get TURN credentials from service (dynamic)
	turnCredentials := p.service.GetTURNCredentials()
	if len(turnCredentials) > 0 {
		logger.Base().Info("Using Twilio TURN server(s) for connection", zap.Int("count", len(turnCredentials)))
	}

	// Use the new Pion template for proper WebRTC handling with dual tracks
	ctx := context.Background()
	result, err := ProcessOfferWithTracks(ctx, offerSDP, stunServers, turnCredentials)
	if err != nil {
		logger.Base().Error("PION TEMPLATE FAILED")
		return "", fmt.Errorf("failed to process offer with Pion template: %w", err)
	}

	logger.Base().Info("Generated SDP answer")

	// Store the peer connection
	p.mutex.Lock()
	p.peerConnections[connectionID] = result.PC
	p.mutex.Unlock()

	// Create Pion Opus writer for output only (single transceiver approach)
	outputWriter := NewPionOpusWriter(result.OutputTrack, result.OutputSender)

	// Store the Pion writer in the connection
	connection := p.service.GetConnection(connectionID)
	if connection != nil {
		connection.SetWAOutputTrack(outputWriter) // Output track (AI->WA)
		logger.Base().Info("Created single output track for connection", zap.String("connection_id", connectionID))
		// Create dedicated Opus decoder for this connection
		if err := p.createDecoderForConnection(connectionID, connection); err != nil {
			logger.Base().Error("Failed to create decoder for connection", zap.String("connection_id", connectionID), zap.Error(err))
			// Continue execution - audio conversion will be skipped but call can proceed
		}

		logger.Base().Info("🎧 WA INPUT: Using OnTrack handler (no separate input track needed)")
		logger.Base().Info("MANUAL RTP: Disabled - using Pion WriteSample only")

		// Start RTCP monitoring for output track
		outputWriter.StartRTCPMonitoring()
		logger.Base().Debug("RTCP Monitor started", zap.String("connection_id", connectionID))
		// Publish WhatsApp audio ready event
		p.service.GetEventBus().Publish(event.WhatsAppAudioReady, &event.WhatsAppEventData{
			ConnectionID: connectionID,
		})
	} else {
		logger.Base().Warn("Connection not found when storing Pion writers", zap.String("connection_id", connectionID))
	}

	// Set up ICE connection state monitoring
	result.PC.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		logger.Base().Info("ICE connection state change", zap.String("state", state.String()))
	})

	// Set up peer connection state monitoring
	result.PC.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		logger.Base().Info("Peer connection state change", zap.String("state", state.String()))
	})

	// Note: OnTrack handler is already set up in pion_webrtc_template.go
	// We need to override it here to add AI forwarding
	result.PC.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		logger.Base().Info("Received track", zap.String("kind", track.Kind().String()), zap.String("connection_id", connectionID), zap.Any("ssrc", track.SSRC()))
		// Forward incoming WhatsApp audio to AI/model
		go p.forwardAudioToAI(connectionID, track, receiver)
	})

	logger.Base().Info("WebRTC setup complete", zap.String("connection_id", connectionID))
	return result.AnswerSDP, nil
}

// GenerateSDPOffer generates a proper SDP offer for outbound calls using Pion WebRTC
func (p *Processor) GenerateSDPOffer(connectionID string) (string, error) {
	logger.Base().Info("Generating SDP offer for outbound call", zap.String("connection_id", connectionID))
	// 1) Build MediaEngine with STEREO Opus codec (matching WhatsApp)
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   config.DefaultSampleRate,
			Channels:    config.DefaultChannelsStereo,
			SDPFmtpLine: "maxaveragebitrate=20000;maxplaybackrate=16000;minptime=20;sprop-maxcapturerate=16000;useinbandfec=1",
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return "", fmt.Errorf("failed to register codec: %v", err)
	}
	logger.Base().Info("Registered STEREO Opus codec for outbound call")

	// Register telephone-event for DTMF support (matching WhatsApp)
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  "audio/telephone-event",
			ClockRate: 8000,
		},
		PayloadType: 126,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return "", fmt.Errorf("failed to register telephone-event: %v", err)
	}
	logger.Base().Info("Registered telephone-event for DTMF support")

	// 2) Configure SettingEngine for outbound offer
	se := webrtc.SettingEngine{}
	// For offers, we set DTLS role to server (passive) or actpass
	// WhatsApp will respond with active

	// Get STUN servers from configuration
	stunServers := p.service.GetSTUNServers()
	if len(stunServers) == 0 {
		// Fallback to default if not configured
		stunServers = []string{
			config.DefaultSTUNServer1,
			config.DefaultSTUNServer2,
		}
		logger.Base().Warn("No STUN servers configured for outbound call, using defaults")
	}

	// Get TURN credentials from service (dynamic)
	turnCredentials := p.service.GetTURNCredentials()

	// Build ICE servers configuration from STUN and TURN servers
	iceServers := make([]webrtc.ICEServer, 0, len(stunServers)+len(turnCredentials))

	// Add STUN servers
	for _, stunURL := range stunServers {
		iceServers = append(iceServers, webrtc.ICEServer{URLs: []string{stunURL}})
	}
	logger.Base().Info("Using STUN servers for outbound call", zap.Strings("stun_servers", stunServers))
	// Add TURN servers from Twilio
	for _, cred := range turnCredentials {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:       cred.URLs,
			Username:   cred.Username,
			Credential: cred.Credential,
		})
		logger.Base().Info("Using Twilio TURN servers for outbound call", zap.Strings("urls", cred.URLs), zap.String("username", cred.Username))
	}

	api := webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(m))
	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers:           iceServers,
		ICECandidatePoolSize: 10, // Pre-gather ICE candidates
	})
	if err != nil {
		return "", fmt.Errorf("failed to create peer connection: %v", err)
	}

	// 3) Create audio track for sending AI audio to WhatsApp
	outputTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: config.DefaultSampleRate,
			Channels:  config.DefaultChannelsStereo,
		}, "audio", "astra-output")
	if err != nil {
		pc.Close()
		return "", fmt.Errorf("failed to create output track: %v", err)
	}
	logger.Base().Info("Created STEREO output track for outbound call")

	// 4) Add transceiver with sendrecv direction
	transceiver, err := pc.AddTransceiverFromTrack(outputTrack,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv})
	if err != nil {
		pc.Close()
		return "", fmt.Errorf("failed to add transceiver: %v", err)
	}
	logger.Base().Info("Added sendrecv transceiver for outbound call")

	// 5) Set up ICE gathering state callback
	gatherComplete := make(chan struct{})
	pc.OnICEGatheringStateChange(func(state webrtc.ICEGathererState) {
		logger.Base().Info("ICE gathering state change", zap.String("state", state.String()))
		if state == webrtc.ICEGathererStateComplete {
			close(gatherComplete)
		}
	})

	// 6) Create SDP offer
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		pc.Close()
		return "", fmt.Errorf("failed to create offer: %v", err)
	}

	// 7) Set local description to start ICE gathering
	if err = pc.SetLocalDescription(offer); err != nil {
		pc.Close()
		return "", fmt.Errorf("failed to set local description: %v", err)
	}

	// 8) Wait for ICE gathering to complete (with timeout)
	select {
	case <-gatherComplete:
		logger.Base().Info("ICE gathering complete for outbound call")
	case <-time.After(5 * time.Second):
		logger.Base().Warn("ICE gathering timeout, using partial candidates")
	}

	// 9) Get the complete SDP offer with ICE candidates
	localDesc := pc.LocalDescription()
	if localDesc == nil {
		pc.Close()
		return "", fmt.Errorf("local description is nil after gathering")
	}

	sdpOffer := localDesc.SDP
	logger.Base().Info("Generated SDP offer", zap.String("connection_id", connectionID), zap.Int("length", len(sdpOffer)))
	// Log SDP preview (first 200 chars)
	previewLen := 200
	if len(sdpOffer) < previewLen {
		previewLen = len(sdpOffer)
	}
	logger.Base().Info("SDP offer preview", zap.String("preview", sdpOffer[:previewLen]), zap.Int("preview_length", previewLen), zap.Int("total_length", len(sdpOffer)))
	// Store the peer connection for later answer processing (DO NOT CLOSE!)
	p.mutex.Lock()
	p.pendingOutbound[connectionID] = &pendingOutboundConn{
		pc:          pc,
		outputTrack: outputTrack,
		sender:      transceiver.Sender(),
	}
	p.mutex.Unlock()
	logger.Base().Info("Stored pending peer connection, waiting for SDP answer", zap.String("connection_id", connectionID))
	return sdpOffer, nil
}

// SendAudioToConnection - legacy compatibility stub
func (p *Processor) SendAudioToConnection(connectionID string, audioData interface{}) error {
	// Legacy compatibility - audio now sent via PionWriter
	return nil
}

// ProcessSDPAnswer processes SDP answer for outbound calls
func (p *Processor) ProcessSDPAnswer(connectionID, answerSDP string) error {
	logger.Base().Info("Processing SDP answer for outbound call", zap.String("connection_id", connectionID))
	// Get pending connection
	p.mutex.Lock()
	pending, exists := p.pendingOutbound[connectionID]
	p.mutex.Unlock()

	if !exists {
		return fmt.Errorf("no pending connection for: %s", connectionID)
	}

	// Check PC state before applying answer
	logger.Base().Debug("PC state before SetRemoteDescription",
		zap.String("connection_state", pending.pc.ConnectionState().String()),
		zap.String("signaling_state", pending.pc.SignalingState().String()),
		zap.String("ice_connection_state", pending.pc.ICEConnectionState().String()),
		zap.String("ice_gathering_state", pending.pc.ICEGatheringState().String()),
	)
	logger.Base().Debug("SDP Answer length", zap.Int("bytes", len(answerSDP)))
	// Log the full SDP for debugging
	logger.Base().Info("Full SDP Answer", zap.String("answer_sdp", answerSDP))

	// Get and log our local description
	if localDesc := pending.pc.LocalDescription(); localDesc != nil {
		logger.Base().Info("Our Local SDP Offer", zap.String("sdp", localDesc.SDP))
	}

	// Apply the answer
	err := pending.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	})
	if err != nil {
		logger.Base().Error("SetRemoteDescription failed")
		pending.pc.Close()
		p.mutex.Lock()
		delete(p.pendingOutbound, connectionID)
		p.mutex.Unlock()
		return fmt.Errorf("failed to set remote description: %w", err)
	}
	logger.Base().Info("Applied SDP answer", zap.String("connection_id", connectionID))
	// Success! Remove from pending and add to active
	p.mutex.Lock()
	delete(p.pendingOutbound, connectionID)
	p.peerConnections[connectionID] = pending.pc
	p.mutex.Unlock()

	// Set up output track
	outputWriter := NewPionOpusWriter(pending.outputTrack, pending.sender)
	connection := p.service.GetConnection(connectionID)
	if connection != nil {
		connection.SetWAOutputTrack(outputWriter)
		logger.Base().Info("WAOutputTrack ready", zap.String("connection_id", connectionID))
		// Create dedicated Opus decoder for this outbound connection
		if err := p.createDecoderForConnection(connectionID, connection); err != nil {
			logger.Base().Error("Failed to create decoder for outbound connection", zap.String("connection_id", connectionID), zap.Error(err))
			// Continue execution - audio conversion will be skipped but call can proceed
		}

		outputWriter.StartRTCPMonitoring()
		p.service.GetEventBus().Publish(event.WhatsAppAudioReady, &event.WhatsAppEventData{
			ConnectionID: connectionID,
		})
	}

	// Set up handlers
	pending.pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		logger.Base().Info("ICE connection state change", zap.String("state", state.String()))
	})
	pending.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		logger.Base().Info("Peer connection state change", zap.String("state", state.String()))
	})
	pending.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		logger.Base().Info("🎧 Received track", zap.String("connection_id", connectionID))
		go p.forwardAudioToAI(connectionID, track, receiver)
	})

	logger.Base().Info("WebRTC established for outbound call", zap.String("connection_id", connectionID))
	return nil
}

// forwardAudioToAI forwards incoming WhatsApp audio to the AI/model
func (p *Processor) forwardAudioToAI(connectionID string, track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	logger.Base().Info("🎧 Starting WhatsApp->AI audio forwarding", zap.String("connection_id", connectionID))
	// Check if AI connection is already ready
	connection := p.service.GetConnection(connectionID)
	modelClient := connection.GetAIWebRTC()
	if connection != nil && modelClient != nil {
		logger.Base().Info("AI connection already ready, starting audio forwarding", zap.String("connection_id", connectionID))
		p.continueAudioForwarding(connectionID, track, receiver, connection)
		return
	}

	logger.Base().Info("AI connection not ready yet, waiting for event", zap.String("connection_id", connectionID))
	// Subscribe to AI connection initialization event with timeout
	err := p.service.GetEventBus().SubscribeWithTimeout(event.AIConnectionInit, func(evt *event.ConnectionEvent) {
		if evt.ConnectionID == connectionID {
			logger.Base().Info("AI connection ready event received", zap.String("connection_id", connectionID))
			connection := p.service.GetConnection(connectionID)
			modelClient := connection.GetAIWebRTC()
			if connection != nil && modelClient != nil {
				p.continueAudioForwarding(connectionID, track, receiver, connection)
			} else {
				logger.Base().Warn("AI connection still not available after event", zap.String("connection_id", connectionID))
			}
		}
	}, 2*time.Second) // 2s timeout, more generous than original 1s

	if err != nil {
		logger.Base().Error("Failed to subscribe to AI connection event", zap.String("connection_id", connectionID), zap.Error(err))
		return
	}

	logger.Base().Info("Subscribed to AI connection event with 2s timeout", zap.String("connection_id", connectionID))
}

// createDecoderForConnection creates a dedicated Opus decoder for a specific connection
// This ensures thread-safety by avoiding shared decoder access across multiple goroutines
func (p *Processor) createDecoderForConnection(connectionID string, connection ConnectionInterface) error {
	if connection == nil {
		return fmt.Errorf("connection is nil")
	}

	// Create Opus decoder for 48kHz, 1 channel (mono)
	decoder, err := gopus.NewDecoder(config.DefaultSampleRate, config.DefaultChannelsMono)
	if err != nil {
		logger.Base().Error("Failed to create Opus decoder for connection", zap.String("connection_id", connectionID), zap.Error(err))
		return fmt.Errorf("failed to create opus decoder: %w", err)
	}

	// Store decoder in connection
	connection.SetOpusDecoder(decoder)

	logger.Base().Info("Created dedicated Opus decoder for connection", zap.String("connection_id", connectionID))
	return nil
}

// continueAudioForwarding continues the audio forwarding process
func (p *Processor) continueAudioForwarding(connectionID string, track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver, connection ConnectionInterface) {
	logger.Base().Info("Starting audio forwarding WhatsApp -> AI", zap.String("connection_id", connectionID))
	// Get audio cache service and check if this channel needs caching
	audioCache := storage.GetAudioCache()
	needsCaching := connection.NeedsAudioCaching()

	// Read RTP packets from WhatsApp and forward to the model
	go func() {
		var frameCount int64       // Count of successfully sent frames
		var suppressionCount int64 // Count of suppressed frames
		defer func() {
			// Cleanup audio cache for this connection (only if cache is available and needed)
			if audioCache != nil && needsCaching {
				audioCache.CleanupConnection(connectionID)
			}
		}()

		for {
			rtpPacket, _, err := track.ReadRTP()
			if err != nil {
				logger.Base().Debug("WhatsApp audio track ended", zap.String("connection_id", connectionID), zap.Error(err))
				return
			}

			// Check atomically if connection is still active
			if connection.IsClosed() {
				logger.Base().Info("🛑 Connection closed, stopping WhatsApp audio processing", zap.String("connection_id", connectionID))
				return
			}

			// Extract Opus payload from RTP packet
			opusPayload := rtpPacket.Payload
			if len(opusPayload) == 0 {
				continue
			}

			// Skip very small frames (likely DTX/silence frames) to reduce processing
			if len(opusPayload) < 3 {
				continue
			}

			// Cache Opus audio asynchronously (only Opus format for efficiency)

			// Convert Opus to PCM16 for the model
			// Use connection-specific decoder to avoid concurrent access issues
			decoder := connection.GetOpusDecoder()

			if decoder == nil {
				// Skip if no decoder available for this connection
				if rtpPacket.SequenceNumber%100 == 0 {
					logger.Base().Warn("No Opus decoder available for connection - skipping audio conversion", zap.String("connection_id", connectionID))
				}
				continue
			}

			// Decode Opus to PCM16 using connection's dedicated decoder
			// Opus frame is typically 20ms at 48kHz = 960 samples per channel
			// Use larger buffer to handle variable frame sizes
			maxSamples := (config.DefaultSampleRate / 1000) * 40 // 40ms buffer (double typical 20ms for safety)
			pcmSamples, err := decoder.Decode(opusPayload, maxSamples, false)
			if err != nil {
				if rtpPacket.SequenceNumber%100 == 0 {
					logger.Base().Error("Failed to decode Opus", zap.String("connection_id", connectionID), zap.Int("payload_bytes", len(opusPayload)), zap.Error(err))
				}
				continue
			}

			// Send PCM16 samples to the model if we got valid samples
			if len(pcmSamples) > 0 {
				// Check if connection is closed before processing audio
				// This is critical because ReadRTP() may still return data from buffer
				// even after the connection is closed
				if connection.IsClosed() {
					logger.Base().Info("🛑 Connection closed, stopping audio forwarding", zap.String("connection_id", connectionID))
					return
				}

				// Cache RTP packet for audio storage (only for WhatsApp/Test channels, not LiveKit)
				if audioCache != nil && needsCaching {
					audioCache.CacheAudioRTP(connectionID, storage.AudioTypeWhatsAppInput, storage.AudioFormatOpus, rtpPacket)
				}

				// Check if we should forward audio to the model
				// If greeting hasn't been sent/completed yet, and connection is very new,
				// suppress user audio to prevent interrupting the greeting
				shouldForward, reason := connection.ShouldForwardAudioToAI()

				if !shouldForward {
					suppressionCount++
					// Log suppression occasionally (every 100 suppressed frames)
					if suppressionCount%100 == 0 {
						logger.Base().Info("🤫 Suppressing user audio during greeting phase", zap.String("reason", reason), zap.String("connection_id", connectionID))
					}
					// Note: LastActivity is updated when audio is successfully forwarded (below)
					// No need to update here during suppression phase as greeting is short-lived
					continue
				}

				// Always send all audio frames - let the model handle silence detection
				modelClient := connection.GetAIWebRTC()
				if modelClient == nil {
					logger.Base().Warn("AI WebRTC client not available", zap.String("connection_id", connectionID))
					continue
				}
				if err := modelClient.SendAudio(pcmSamples); err != nil {
					// If SendAudio fails, check if connection is closed
					// If so, exit the loop instead of continuing
					if connection.IsClosed() {
						logger.Base().Error("🛑 Connection closed, SendAudio failed", zap.String("connection_id", connectionID))
						return
					}
					logger.Base().Error("SendAudio failed", zap.String("connection_id", connectionID), zap.Error(err))
					// Continue processing even if SendAudio fails once
					// (might be temporary network issue)
				} else {
					frameCount++
					// Log successful decode occasionally
					if frameCount%100 == 0 {
						logger.Base().Info("Decoded Opus to PCM16", zap.String("connection_id", connectionID), zap.Int("pcm_samples", len(pcmSamples)), zap.Int("opus_bytes", len(opusPayload)))
					}

					// Update LastActivity periodically to prevent connection timeout
					// Update every ~1 second (50 frames * 20ms)
					// Check connection state before updating to prevent updating after closure
					if frameCount%50 == 0 {
						if !connection.IsClosed() {
							connection.UpdateLastActivity()
						}
					}
				}
			} else {
				if rtpPacket.SequenceNumber%100 == 0 {
					logger.Base().Warn("Opus decode returned 0 samples", zap.String("connection_id", connectionID), zap.Int("opus_bytes", len(opusPayload)))
				}
			}

			// Log successful audio forwarding (reduce frequency to avoid spam)
			if frameCount > 0 && frameCount%25 == 0 {
				logger.Base().Info("WA->AI: audio frames forwarded", zap.String("connection_id", connectionID), zap.Int64("frame_count", frameCount))
			}
		}
	}()
}

// getTwilioTURNCredentials is deprecated, use service.GetTURNCredentials() instead
func (p *Processor) getTwilioTURNCredentials() []TURNCredentials {
	return p.service.GetTURNCredentials()
}

// CleanupConnection cleans up WebRTC resources for a connection
func (p *Processor) CleanupConnection(connectionID string) {
	// Extract connections from maps while holding the lock
	p.mutex.Lock()
	var activePc *webrtc.PeerConnection
	var pendingPc *webrtc.PeerConnection

	// Get active connection
	if pc, exists := p.peerConnections[connectionID]; exists {
		activePc = pc
		delete(p.peerConnections, connectionID)
	}

	// Get pending outbound connection
	if pending, exists := p.pendingOutbound[connectionID]; exists {
		pendingPc = pending.pc
		delete(p.pendingOutbound, connectionID)
	}
	p.mutex.Unlock()

	// Close connections after releasing the lock to avoid deadlock
	if activePc != nil {
		if err := activePc.Close(); err != nil {
			logger.Base().Error("Error closing peer connection", zap.String("connection_id", connectionID), zap.Error(err))
		}
		logger.Base().Info("🧹 Cleaned up WebRTC connection", zap.String("connection_id", connectionID))
	}

	if pendingPc != nil {
		if err := pendingPc.Close(); err != nil {
			logger.Base().Error("Error closing pending connection", zap.String("connection_id", connectionID), zap.Error(err))
		}
		logger.Base().Info("🧹 Cleaned up pending outbound", zap.String("connection_id", connectionID))
	}
}
