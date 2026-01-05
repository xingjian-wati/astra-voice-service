package webrtc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appconfig "github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"go.uber.org/zap"
	"layeh.com/gopus"
)

const (
	DefaultDataChannelName = "oai-events" // OpenAI default
)

// Client handles model (e.g., OpenAI) Realtime API via WebRTC
type Client struct {
	config          *appconfig.WebSocketConfig
	peerConnection  *webrtc.PeerConnection
	dataChannel     *webrtc.DataChannel
	audioTrack      *webrtc.TrackLocalStaticSample
	sdpExchanger    func(ctx context.Context, sdp, token string) (string, error)
	dataChannelName string

	// Audio channels
	AudioOut chan []int16
	AudioIn  chan []int16

	// Event handling
	EventHandler func(event map[string]interface{})

	// Audio handling
	AudioTrackHandler func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)

	// Data channel opening
	OnDataChannelOpen func()

	// Opus encoder for PCM16 -> Opus conversion
	opusEncoder *gopus.Encoder

	// Connection state
	connected bool
	closed    bool
}

// NewClient creates a new WebRTC client for the model Realtime API
func NewClient(cfg *appconfig.WebSocketConfig) *Client {
	// Create Opus encoder for 48kHz, 1 channel (mono)
	encoder, err := gopus.NewEncoder(appconfig.DefaultSampleRate, appconfig.DefaultChannelsMono, gopus.Audio)
	if err != nil {
		logger.Base().Error("Failed to create Opus encoder", zap.Error(err))
		encoder = nil
	} else {
		// Set higher bitrate for better audio quality
		encoder.SetBitrate(appconfig.DefaultOpusBitrate)
		logger.Base().Info("Created Opus encoder for model audio",
			zap.Int("sample_rate", appconfig.DefaultSampleRate),
			zap.Int("channels", appconfig.DefaultChannelsMono),
			zap.Int("bitrate", appconfig.DefaultOpusBitrate))
	}

	return &Client{
		config:          cfg,
		AudioOut:        make(chan []int16, 256), // Increased buffer for better quality
		AudioIn:         make(chan []int16, 256), // Increased buffer for better quality
		opusEncoder:     encoder,
		connected:       false,
		closed:          false,
		dataChannelName: DefaultDataChannelName,
	}
}

// SetSDPExchanger injects a provider-specific SDP exchange function.
func (c *Client) SetSDPExchanger(fn func(ctx context.Context, sdp, token string) (string, error)) {
	c.sdpExchanger = fn
}

// SetDataChannelName sets the name of the data channel to be created.
func (c *Client) SetDataChannelName(name string) {
	c.dataChannelName = name
}

// Connect establishes WebRTC connection to the model Realtime API
func (c *Client) Connect(ctx context.Context, ephemeralToken string) error {
	logger.Base().Info("Establishing WebRTC connection to model Realtime API")

	// Create WebRTC configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{appconfig.DefaultSTUNServer1},
			},
		},
	}

	// Create peer connection
	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return fmt.Errorf("failed to create peer connection: %w", err)
	}
	c.peerConnection = pc

	// Set up connection state monitoring
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		logger.Base().Info("WebRTC connection state change", zap.String("state", state.String()))
		if state == webrtc.PeerConnectionStateConnected {
			c.connected = true
			logger.Base().Info("WebRTC connection established with model provider")
		} else if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			c.connected = false
		}
	})

	// Set up ICE connection state monitoring
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		logger.Base().Debug("ICE connection state change", zap.String("state", state.String()))
	})

	// Create data channel for events
	dc, err := pc.CreateDataChannel(c.dataChannelName, nil)
	if err != nil {
		return fmt.Errorf("failed to create data channel: %w", err)
	}
	c.dataChannel = dc

	// Set up data channel event handlers
	dc.OnOpen(func() {
		logger.Base().Info("Data channel opened")
		c.connected = true
		if c.OnDataChannelOpen != nil {
			c.OnDataChannelOpen()
		}
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		c.handleServerEvent(msg.Data)
	})

	// Create audio track for sending audio to the model
	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: appconfig.DefaultSampleRate,
			Channels:  appconfig.DefaultChannelsMono,
		},
		"audio",
		"ai-input",
	)
	if err != nil {
		return fmt.Errorf("failed to create audio track: %w", err)
	}
	c.audioTrack = audioTrack

	// Add audio track to peer connection
	_, err = pc.AddTrack(audioTrack)
	if err != nil {
		return fmt.Errorf("failed to add audio track: %w", err)
	}

	// Handle incoming audio tracks from the model
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		logger.Base().Info("Received audio track from model", zap.String("kind", track.Kind().String()))
		go c.handleIncomingAudio(track)
	})

	// Create offer
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("failed to create offer: %w", err)
	}

	// Set local description
	err = pc.SetLocalDescription(offer)
	if err != nil {
		return fmt.Errorf("failed to set local description: %w", err)
	}

	// Send offer to provider and get answer
	if c.sdpExchanger == nil {
		return fmt.Errorf("sdp exchanger not configured")
	}

	answer, err := c.sdpExchanger(ctx, offer.SDP, ephemeralToken)
	if err != nil {
		return fmt.Errorf("failed to exchange SDP: %w", err)
	}

	// Set remote description
	err = pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer,
	})
	if err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	logger.Base().Info("WebRTC connection setup complete")
	return nil
}

// handleIncomingAudio processes incoming audio from the model
func (c *Client) handleIncomingAudio(track *webrtc.TrackRemote) {
	for {
		if c.closed {
			return
		}

		// Read RTP packet
		_, _, err := track.ReadRTP()
		if err != nil {
			logger.Base().Error("Error reading RTP packet", zap.Error(err))
			return
		}

		// TODO: Decode Opus audio and send to AudioOut channel
		// This requires implementing Opus decoding
		// Removed excessive logging for audio packets
	}
}

// handleServerEvent processes events from the model via data channel
func (c *Client) handleServerEvent(data []byte) {
	var event map[string]interface{}
	if err := json.Unmarshal(data, &event); err != nil {
		logger.Base().Error("Failed to parse server event", zap.Error(err))
		return
	}

	eventType, _ := event["type"].(string)

	// Print input_audio_buffer.* and response.* events for debugging
	if strings.HasPrefix(eventType, "input_audio_buffer.") || strings.HasPrefix(eventType, "response.") {
		logger.Base().Debug("Model DataChannel event", zap.String("type", eventType), zap.ByteString("payload", data))
	} else {
		logger.Base().Debug("Model event", zap.String("type", eventType))
	}

	// Call event handler if set
	if c.EventHandler != nil {
		c.EventHandler(event)
	}
}

// SendEvent sends an event to the model via data channel
func (c *Client) SendEvent(event map[string]interface{}) error {
	if c.dataChannel == nil || c.dataChannel.ReadyState() != webrtc.DataChannelStateOpen {
		return fmt.Errorf("data channel not ready")
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	return c.dataChannel.Send(data)
}

// SendAudio sends audio data to the model
func (c *Client) SendAudio(samples []int16) error {
	// Check if connection is closed first
	if c.closed {
		return fmt.Errorf("connection is closed")
	}

	if c.audioTrack == nil {
		return fmt.Errorf("audio track not available")
	}

	if c.opusEncoder == nil {
		return fmt.Errorf("opus encoder not available")
	}

	if len(samples) == 0 {
		return nil // Nothing to send
	}

	// Encode PCM16 samples to Opus (increased bitrate to ~24kbps)
	frameSize := len(samples)
	// Check if frame size is valid for 48kHz Opus
	// Valid sizes: 120 (2.5ms), 240 (5ms), 480 (10ms), 960 (20ms), 1920 (40ms), 2880 (60ms)
	switch frameSize {
	case 120, 240, 480, 960, 1920, 2880:
		// Valid frame size
	default:
		return fmt.Errorf("invalid frame size for %dHz Opus: %d samples", appconfig.DefaultSampleRate, frameSize)
	}

	opusData, err := c.opusEncoder.Encode(samples, frameSize, 6000) // max 6000 bytes
	if err != nil {
		return fmt.Errorf("failed to encode audio to Opus: %w", err)
	}

	if len(opusData) == 0 {
		return nil // No data produced
	}

	// Calculate duration based on frame size
	duration := time.Duration(frameSize) * time.Second / time.Duration(appconfig.DefaultSampleRate)

	// Create media sample with Opus data
	sample := media.Sample{
		Data:     opusData,
		Duration: duration,
	}

	// Send via audio track
	if err := c.audioTrack.WriteSample(sample); err != nil {
		return fmt.Errorf("failed to write audio sample: %w", err)
	}

	return nil
}

// Close closes the WebRTC connection
func (c *Client) Close() error {
	c.closed = true
	c.connected = false

	if c.dataChannel != nil {
		c.dataChannel.Close()
	}

	if c.peerConnection != nil {
		return c.peerConnection.Close()
	}

	logger.Base().Info("WebRTC connection closed")
	return nil
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	return c.connected && !c.closed
}

// Initialize initializes the WebRTC connection with the model provider
func (c *Client) Initialize(ctx context.Context, token string) error {
	logger.Base().Info("Initializing WebRTC connection to model provider")

	// Create peer connection
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{appconfig.DefaultSTUNServer1}},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return fmt.Errorf("failed to create peer connection: %w", err)
	}
	c.peerConnection = pc

	// Create single sendrecv transceiver
	tr, err := pc.AddTransceiverFromKind(
		webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv},
	)
	if err != nil {
		return fmt.Errorf("failed to add sendrecv transceiver: %w", err)
	}

	// Create sending track (Opus/48k/mono)
	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: appconfig.DefaultSampleRate,
			Channels:  appconfig.DefaultChannelsMono,
		},
		"audio", "ai-input",
	)
	if err != nil {
		return fmt.Errorf("failed to create audio track: %w", err)
	}
	c.audioTrack = audioTrack

	// Bind to sendrecv transceiver
	if err := tr.Sender().ReplaceTrack(audioTrack); err != nil {
		return fmt.Errorf("failed to replace track: %w", err)
	}
	logger.Base().Info("Added single sendrecv transceiver with ReplaceTrack")

	// Create data channel
	dc, err := pc.CreateDataChannel(c.dataChannelName, nil)
	if err != nil {
		return fmt.Errorf("failed to create data channel: %w", err)
	}
	c.dataChannel = dc

	// Set up data channel handlers
	dc.OnOpen(func() {
		logger.Base().Info("Model data channel opened")
		c.connected = true

		if c.OnDataChannelOpen != nil {
			c.OnDataChannelOpen()
		}

		// Session already configured via ephemeral token - no need for session.update
		logger.Base().Info("Session already configured via ephemeral token")

		// No longer auto-trigger response.create, wait for user to speak before responding
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString && c.EventHandler != nil {
			var event map[string]interface{}
			if err := json.Unmarshal(msg.Data, &event); err == nil {
				c.EventHandler(event)
			}
		}
	})

	// Set up audio track handler for receiving model audio
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		logger.Base().Info("Received audio track from model", zap.String("kind", track.Kind().String()))
		if c.AudioTrackHandler != nil {
			c.AudioTrackHandler(track, receiver)
		}
	})

	// Create offer
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("failed to create offer: %w", err)
	}

	// Set local description
	if err := pc.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait for ICE gathering
	<-webrtc.GatheringCompletePromise(pc)

	// Exchange SDP with provider
	if c.sdpExchanger == nil {
		return fmt.Errorf("sdp exchanger not configured")
	}

	answerSDP, err := c.sdpExchanger(ctx, pc.LocalDescription().SDP, token)
	if err != nil {
		return fmt.Errorf("failed to exchange SDP: %w", err)
	}

	// Set remote description
	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}

	if err := pc.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	logger.Base().Info("WebRTC connection initialized successfully")
	return nil
}

// AddConversationHistory adds conversation history to the current session
func (c *Client) AddConversationHistory(messages []ConversationMessage) error {
	if !c.connected {
		return fmt.Errorf("client not connected")
	}

	for _, msg := range messages {
		// Create conversation item for each message
		item := map[string]interface{}{
			"type": "conversation.item.create",
			"item": map[string]interface{}{
				"type": "message",
				"role": msg.Role,
				"content": []map[string]interface{}{
					{
						"type": "input_text",
						"text": msg.Content,
					},
				},
			},
		}

		if err := c.SendEvent(item); err != nil {
			logger.Base().Error("Failed to add conversation history item", zap.Error(err))
			return err
		}
	}

	logger.Base().Info("Added conversation history items", zap.Int("count", len(messages)))
	return nil
}

// ConversationMessage represents a message in conversation history
type ConversationMessage struct {
	Role      string    `json:"role"`      // "user", "assistant", "system"
	Content   string    `json:"content"`   // The message content
	Timestamp time.Time `json:"timestamp"` // When the message was created
}
