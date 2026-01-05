package livekit

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/core/event"
	"github.com/ClareAI/astra-voice-service/internal/core/model/provider"
	"github.com/ClareAI/astra-voice-service/internal/services/call"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// LiveKitRoom stores LiveKit-specific resources for a connection
type LiveKitRoom struct {
	Room      *lksdk.Room
	RoomName  string
	CreatedAt time.Time
	EgressID  string        // Egress ID for recording
	ReadyChan chan struct{} // Notification channel: room is ready
}

// RoomManager manages LiveKit rooms and audio routing
type RoomManager struct {
	config *LiveKitConfig
	rooms  map[string]*LiveKitRoom // connectionID -> LiveKit room
	mutex  sync.RWMutex

	service        *call.WhatsAppCallService
	audioProcessor *AudioProcessor
	egressClient   *lksdk.EgressClient // For egress operations
}

// NewRoomManager creates a new LiveKit room manager
func NewRoomManager(config *LiveKitConfig, service *call.WhatsAppCallService, _ interface{}) (*RoomManager, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid LiveKit config: %w", err)
	}

	audioProcessor, err := NewAudioProcessor()
	if err != nil {
		return nil, fmt.Errorf("failed to create audio processor: %w", err)
	}

	// Initialize egress client for recording
	egressClient := lksdk.NewEgressClient(config.ServerURL, config.APIKey, config.APISecret)

	rm := &RoomManager{
		config:         config,
		rooms:          make(map[string]*LiveKitRoom),
		service:        service,
		audioProcessor: audioProcessor,
		egressClient:   egressClient,
	}

	logger.Base().Info("LiveKit RoomManager initialized", zap.String("server_url", config.ServerURL))
	return rm, nil
}

// GenerateToken generates a LiveKit access token for a participant
func (rm *RoomManager) GenerateToken(roomName, participantName string) (string, error) {
	at := auth.NewAccessToken(rm.config.APIKey, rm.config.APISecret)

	// Explicitly set permissions
	canPublish := true
	canSubscribe := true

	grant := &auth.VideoGrant{
		RoomJoin:     true,
		Room:         roomName,
		CanPublish:   &canPublish,   // Explicitly allow publishing
		CanSubscribe: &canSubscribe, // Explicitly allow subscribing
	}

	at.SetVideoGrant(grant).
		SetIdentity(participantName).
		SetValidFor(2 * time.Hour)

	token, err := at.ToJWT()
	if err != nil {
		return "", fmt.Errorf("failed to generate JWT: %w", err)
	}

	return token, nil
}

// JoinRoomAsBot joins a LiveKit room as a bot to handle audio routing
// triggerGreetingImmediately: true = trigger greeting immediately (JoinRoom), false = wait for signal (CreateRoom)
func (rm *RoomManager) JoinRoomAsBot(connectionID, roomName string, triggerGreetingImmediately bool) error {
	logger.Base().Info("Bot joining room", zap.String("room_name", roomName), zap.String("connection_id", connectionID), zap.Bool("trigger_greeting_immediately", triggerGreetingImmediately))

	// Create room callback to handle audio tracks
	roomCallback := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: func(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
				logger.Base().Info("Track subscribed", zap.String("kind", track.Kind().String()), zap.String("participant", rp.Identity()))
				if track.Kind() == webrtc.RTPCodecTypeAudio {
					// Forward LiveKit audio to the model
					go rm.forwardLiveKitAudio(connectionID, track)
				}
			},
		},
		OnParticipantConnected: func(rp *lksdk.RemoteParticipant) {
			participantIdentity := rp.Identity()

			// Filter out self (bot)
			if participantIdentity == fmt.Sprintf("%s%s", config.DefaultLiveKitBotPrefix, connectionID) {
				return
			}

			logger.Base().Info("👤 Participant connected", zap.String("participant_identity", participantIdentity))

			// Check if participant is the expected target (must start with filter)
			if !strings.HasPrefix(participantIdentity, config.ParticipantPrefixFilter) {
				logger.Base().Info("Participant does not match target criteria, ignoring for greeting",
					zap.String("participant_identity", participantIdentity),
					zap.String("prefix_required", config.ParticipantPrefixFilter))
				return
			}

			// Trigger participant_joined event
			logger.Base().Info("📊 participant_joined", zap.String("participant_identity", participantIdentity), zap.String("room_name", roomName), zap.String("connection_id", connectionID))

			// Only trigger greeting in CreateRoom mode (JoinRoom mode already triggered it)
			if !triggerGreetingImmediately {
				logger.Base().Info("Target participant joined, triggering AI greeting (CreateRoom mode)")
				go rm.triggerGreetingForParticipant(connectionID, participantIdentity)
			} else {
				logger.Base().Warn("Greeting already triggered (JoinRoom mode), skipping")
			}

		},
		OnParticipantDisconnected: func(rp *lksdk.RemoteParticipant) {
			participantIdentity := rp.Identity()
			logger.Base().Info("👋 Participant disconnected", zap.String("participant_identity", participantIdentity))

			// Trigger participant_left event
			logger.Base().Info("📊 participant_left", zap.String("participant_identity", participantIdentity), zap.String("room_name", roomName), zap.String("connection_id", connectionID))

			// Cleanup when last participant leaves
			rm.CleanupRoom(connectionID)
		},
		OnDisconnected: func() {
			logger.Base().Info("🔌 Bot disconnected from room", zap.String("room_name", roomName))
		},
	}

	// Create room object with notification channel
	readyChan := make(chan struct{})
	rm.mutex.Lock()
	rm.rooms[connectionID] = &LiveKitRoom{
		Room:      nil, // Placeholder
		RoomName:  roomName,
		CreatedAt: time.Now(),
		ReadyChan: readyChan,
	}
	rm.mutex.Unlock()

	// Connect to room as bot
	room, err := lksdk.ConnectToRoom(rm.config.ServerURL, lksdk.ConnectInfo{
		APIKey:              rm.config.APIKey,
		APISecret:           rm.config.APISecret,
		RoomName:            roomName,
		ParticipantIdentity: fmt.Sprintf("%s%s", config.DefaultLiveKitBotPrefix, connectionID),
	}, roomCallback)

	if err != nil {
		// Connection failed, cleanup room
		rm.mutex.Lock()
		delete(rm.rooms, connectionID)
		rm.mutex.Unlock()
		return fmt.Errorf("failed to connect to room: %w", err)
	}

	// Update room object and send ready notification
	rm.mutex.Lock()
	if lkRoom, exists := rm.rooms[connectionID]; exists {
		lkRoom.Room = room
		close(lkRoom.ReadyChan) // Notify all waiters: room is ready
	}
	rm.mutex.Unlock()

	logger.Base().Info("Bot joined room", zap.String("room_name", roomName))

	// Manually trigger room_started event
	logger.Base().Info("📊 room_started", zap.String("room_name", roomName), zap.String("connection_id", connectionID))

	// Start forwarding model audio to LiveKit
	go rm.forwardAIAudio(connectionID, room)

	// JoinRoom mode (triggerGreetingImmediately = true) doesn't need manual trigger
	// as AI will start conversation automatically after connection
	if triggerGreetingImmediately {
		logger.Base().Info("JoinRoom mode: AI will auto-start (no signal control)")
	} else {
		// CreateRoom mode: Check for existing participants
		logger.Base().Debug("CreateRoom mode: Checking for existing participants to trigger greeting...")

		participants := room.GetParticipants()
		if len(participants) > 0 {
			for _, p := range participants {
				participantIdentity := p.Identity()

				// Filter out self (bot)
				if participantIdentity == fmt.Sprintf("%s%s", config.DefaultLiveKitBotPrefix, connectionID) {
					continue
				}

				logger.Base().Info("👤 Found existing participant", zap.String("participant_identity", participantIdentity))

				// Check if participant is the expected target
				if !strings.HasPrefix(participantIdentity, config.ParticipantPrefixFilter) {
					logger.Base().Info("Existing participant does not match target criteria, ignoring",
						zap.String("participant_identity", participantIdentity),
						zap.String("prefix_required", config.ParticipantPrefixFilter))
					continue
				}

				logger.Base().Info("Existing target participant found, triggering AI greeting (CreateRoom mode)")

				// Trigger greeting
				go rm.triggerGreetingForParticipant(connectionID, participantIdentity)
			}
		}
	}

	return nil
}

// forwardLiveKitAudio forwards audio from LiveKit to the model
func (rm *RoomManager) forwardLiveKitAudio(connectionID string, track *webrtc.TrackRemote) {
	logger.Base().Info("Starting LiveKit -> AI audio forwarding", zap.String("connection_id", connectionID))

	// Check if connection and model are already ready
	connection := rm.service.GetConnection(connectionID)
	if connection != nil && connection.GetAIWebRTC() != nil {
		logger.Base().Info("Connection and model already ready, starting audio forwarding", zap.String("connection_id", connectionID))
		// Type assert to concrete type for ForwardLiveKitAudioToAI
		if conn, ok := connection.(*call.WhatsAppCallConnection); ok {
			rm.audioProcessor.ForwardLiveKitAudioToAI(connectionID, track, conn)
		}
		return
	}

	logger.Base().Info("Connection or model not ready yet, waiting for event", zap.String("connection_id", connectionID))

	// Subscribe to model connection initialization event with timeout
	err := rm.service.GetEventBus().SubscribeWithTimeout(event.AIConnectionInit, func(evt *event.ConnectionEvent) {
		if evt.ConnectionID == connectionID {
			logger.Base().Info("AI connection ready event received", zap.String("connection_id", connectionID))
			conn := rm.service.GetConnection(connectionID)
			if conn != nil && conn.GetAIWebRTC() != nil {
				// Type assert to concrete type for ForwardLiveKitAudioToAI
				if callConn, ok := conn.(*call.WhatsAppCallConnection); ok {
					rm.audioProcessor.ForwardLiveKitAudioToAI(connectionID, track, callConn)
				}
			} else {
				logger.Base().Warn("Connection or model still not available after event", zap.String("connection_id", connectionID))
			}
		}
	}, 10*time.Second)

	if err != nil {
		logger.Base().Error("Failed to subscribe to AI connection event")
		return
	}

	logger.Base().Info("Subscribed to AI connection event", zap.String("connection_id", connectionID))
}

// CleanupRoom cleans up a LiveKit room
func (rm *RoomManager) CleanupRoom(connectionID string) {
	rm.mutex.Lock()
	room, exists := rm.rooms[connectionID]
	if !exists {
		rm.mutex.Unlock()
		return
	}
	delete(rm.rooms, connectionID)
	rm.mutex.Unlock()

	duration := time.Since(room.CreatedAt).Seconds()

	logger.Base().Info("Room finished", zap.String("room_name", room.RoomName), zap.String("connection_id", connectionID), zap.Float64("duration", duration))

	if room.Room != nil {
		room.Room.Disconnect()
	}

	// Also cleanup the service connection and broadcast to other pods
	if rm.service != nil {
		rm.service.NotifyCleanup(context.Background(), connectionID)
	}

	logger.Base().Info("Room cleaned up", zap.String("connection_id", connectionID))
}

// forwardAIAudio sets up LiveKit audio output for the model
func (rm *RoomManager) forwardAIAudio(connectionID string, room *lksdk.Room) {
	logger.Base().Info("🔊 Setting up AI -> LiveKit audio output", zap.String("connection_id", connectionID))

	// Check if connection and model are already ready
	connection := rm.service.GetConnection(connectionID)
	if connection != nil && connection.GetAIWebRTC() != nil {
		logger.Base().Info("Connection and model already ready, setting up audio output", zap.String("connection_id", connectionID))
		if conn, ok := connection.(*call.WhatsAppCallConnection); ok {
			rm.setupLiveKitAudioOutput(connectionID, room, conn)
		}
		return
	}

	logger.Base().Info("Connection or model not ready yet, waiting for event", zap.String("connection_id", connectionID))

	// Subscribe to model connection initialization event with timeout
	err := rm.service.GetEventBus().SubscribeWithTimeout(event.AIConnectionInit, func(evt *event.ConnectionEvent) {
		if evt.ConnectionID == connectionID {
			logger.Base().Info("AI connection ready event received for audio output", zap.String("connection_id", connectionID))
			conn := rm.service.GetConnection(connectionID)
			if conn != nil && conn.GetAIWebRTC() != nil {
				if callConn, ok := conn.(*call.WhatsAppCallConnection); ok {
					rm.setupLiveKitAudioOutput(connectionID, room, callConn)
				}
			} else {
				logger.Base().Warn("Connection or model still not available after event", zap.String("connection_id", connectionID))
			}
		}
	}, 10*time.Second)

	if err != nil {
		logger.Base().Error("Failed to subscribe to AI connection event")
		return
	}

	logger.Base().Info("Subscribed to AI connection event", zap.String("connection_id", connectionID))
}

// setupLiveKitAudioOutput creates and publishes audio track for LiveKit
func (rm *RoomManager) setupLiveKitAudioOutput(connectionID string, room *lksdk.Room, connection *call.WhatsAppCallConnection) {
	// Create local audio track for publishing to LiveKit
	// Configure for optimal low-latency audio: 20ms Opus frames, mono, 48kHz
	// Configure Opus parameters via SDPFmtpLine to ensure continuous audio stream
	audioTrack, err := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: config.DefaultSampleRate,   // 48kHz sample rate
		Channels:  config.DefaultChannelsMono, // Mono (single channel)
		// Critical Opus parameters:
		// - minptime=20: minimum packet time 20ms (align with model)
		// - useinbandfec=1: enable in-band forward error correction (improve quality)
		// - usedtx=0: disable DTX (ensure continuous audio stream, prevent VAD delay)
		SDPFmtpLine: "minptime=20;useinbandfec=1;usedtx=0",
	})
	if err != nil {
		logger.Base().Error("Failed to create audio track")
		return
	}

	logger.Base().Info("Audio track created: Opus 48kHz mono, 20ms frames, DTX disabled")

	// Publish track to room
	if _, err := room.LocalParticipant.PublishTrack(audioTrack, &lksdk.TrackPublicationOptions{
		Name: "ai-audio",
	}); err != nil {
		logger.Base().Error("Failed to publish track")
		return
	}

	logger.Base().Info("Model audio track published")

	// Create LiveKitOpusWriter and set as WAOutputTrack
	// This allows the model's existing audio handler to write directly to LiveKit
	livekitWriter := NewLiveKitOpusWriter(audioTrack, connectionID)
	connection.WAOutputTrack = livekitWriter

	logger.Base().Info("WAOutputTrack set, model handler will use it automatically")

	// Publish WhatsAppAudioReady event to trigger the model audio bridge
	if rm.service != nil {
		eventBus := rm.service.GetEventBus()
		if eventBus != nil {
			eventBus.Publish(config.EventWhatsAppAudioReady, map[string]interface{}{
				"connection_id": connectionID,
			})
			logger.Base().Info("Published WhatsAppAudioReady event")
		}
	}
}

// GetConfigInternal returns the LiveKit configuration
func (rm *RoomManager) GetConfigInternal() *LiveKitConfig {
	return rm.config
}

// GetRoomCount returns the number of active rooms
func (rm *RoomManager) GetRoomCount() int {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()
	return len(rm.rooms)
}

// triggerGreetingForParticipant triggers greeting signal when a participant joins
// Uses signal control mechanism: AI waits for signal before sending greeting
func (rm *RoomManager) triggerGreetingForParticipant(connectionID, participantName string) {
	logger.Base().Info("Participant joined, sending greeting signal", zap.String("participant_name", participantName))

	// Get connection to determine provider type
	connection := rm.service.GetConnection(connectionID)
	providerType := provider.ProviderTypeOpenAI
	if connection != nil {
		if conn, ok := connection.(*call.WhatsAppCallConnection); ok {
			providerType = conn.ModelProvider
		}
	}

	// Trigger greeting signal (Model handler is waiting for this)
	if modelHandler, err := rm.service.GetModelHandler(providerType); err == nil && modelHandler != nil {
		modelHandler.TriggerGreeting(connectionID)
		logger.Base().Info("📊 greeting_triggered", zap.String("connection_id", connectionID), zap.String("participant_name", participantName))
	} else {
		logger.Base().Warn("Model handler not available", zap.String("connection_id", connectionID), zap.String("provider", string(providerType)))
	}
}

// startRecordingForRoom starts room recording when all participants joined
// Uses channel notification mechanism, event-driven
func (rm *RoomManager) startRecordingForRoom(connectionID, roomName string) {
	rm.mutex.RLock()
	room, exists := rm.rooms[connectionID]
	rm.mutex.RUnlock()

	if !exists {
		logger.Base().Warn("Room not found for recording", zap.String("connection_id", connectionID))
		return
	}

	// Wait for room ready notification
	select {
	case <-room.ReadyChan:
		logger.Base().Info("Room ready, starting recording", zap.String("room_name", roomName))
	case <-time.After(5 * time.Second):
		logger.Base().Warn("Timeout waiting for room to be ready", zap.String("connection_id", connectionID))
		return
	}

	// Check if already has egress
	rm.mutex.RLock()
	egressID := room.EgressID
	rm.mutex.RUnlock()

	if egressID != "" {
		logger.Base().Info("Recording already started for room", zap.String("room_name", roomName))
		return
	}

	// Start recording
	logger.Base().Info("🎬 Starting recording for room", zap.String("room_name", roomName))
	egressID, err := rm.StartEgress(connectionID, roomName)
	if err != nil {
		logger.Base().Error("Failed to start recording")
		return
	}

	logger.Base().Info("Recording started", zap.String("egress_id", egressID), zap.String("room_name", roomName))
}

// StartEgress starts recording (calls LiveKit API)
func (rm *RoomManager) StartEgress(connectionID, roomName string) (string, error) {
	logger.Base().Info("🎬 Starting audio egress for room", zap.String("room_name", roomName))

	// Read base64 encoded GCS credentials from environment variable
	credentialsBase64 := os.Getenv("GOOGLE_STORAGE_LIVEKIT_CLOUD_ACCOUNT_JSON_BASE64")
	if credentialsBase64 == "" {
		logger.Base().Warn("GOOGLE_STORAGE_LIVEKIT_CLOUD_ACCOUNT_JSON_BASE64 not set, skipping GCS upload")
		return "", fmt.Errorf("GCS credentials not configured")
	}

	// Base64 decode
	credentialsJson, err := base64.StdEncoding.DecodeString(credentialsBase64)
	if err != nil {
		logger.Base().Error("Failed to decode base64 credentials")
		return "", fmt.Errorf("failed to decode credentials: %w", err)
	}

	logger.Base().Info("GCS credentials loaded from environment variable")

	// Create egress request, audio only
	fileOutput := &livekit.EncodedFileOutput{
		FileType: livekit.EncodedFileType_OGG,                                                                        // Output OGG format (Opus)
		Filepath: fmt.Sprintf("%s%s%s", config.DefaultEgressPathPrefix, connectionID, config.DefaultEgressExtension), // Use connectionID, consistent directory
	}

	// If GCS bucket is configured, upload to GCS
	if rm.config.GCSBucket != "" {
		fileOutput.Output = &livekit.EncodedFileOutput_Gcp{
			Gcp: &livekit.GCPUpload{
				Bucket:      rm.config.GCSBucket,
				Credentials: string(credentialsJson),
			},
		}
		logger.Base().Info("Egress will upload to GCS bucket", zap.String("gcs_bucket", rm.config.GCSBucket))
	}

	req := &livekit.RoomCompositeEgressRequest{
		RoomName:  roomName,
		AudioOnly: true, // Recording audio only

		// Output format (use FileOutputs array)
		FileOutputs: []*livekit.EncodedFileOutput{fileOutput},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start Egress recording
	info, err := rm.egressClient.StartRoomCompositeEgress(ctx, req)
	if err != nil {
		logger.Base().Error("Failed to start egress")
		return "", fmt.Errorf("failed to start egress: %w", err)
	}

	// Log detailed egress info
	egressID := info.EgressId
	logger.Base().Info("Egress started successfully!")
	logger.Base().Info("Egress Details",
		zap.String("egress_id", egressID),
		zap.String("room_name", info.RoomName),
		zap.String("status", info.Status.String()),
		zap.String("room_id", info.RoomId))

	// Log file info
	if len(info.FileResults) > 0 {
		logger.Base().Info("File results", zap.Int("count", len(info.FileResults)))
		for _, file := range info.FileResults {
			logger.Base().Info("File info", zap.String("filename", file.Filename), zap.Int64("size", file.Size), zap.Float64("duration_sec", float64(file.Duration)/1e9))
		}
	} else {
		logger.Base().Info("File Results: recording in progress")
	}

	// Log stream info
	if len(info.StreamResults) > 0 {
		logger.Base().Info("Stream results", zap.Int("count", len(info.StreamResults)))
	}

	// Log error info
	if info.Error != "" {
		logger.Base().Error("Error", zap.String("error", info.Error))
	}
	// Save egress ID
	rm.mutex.Lock()
	if room, exists := rm.rooms[connectionID]; exists {
		room.EgressID = egressID
	}
	rm.mutex.Unlock()

	return egressID, nil
}

// stopEgress stops recording (calls LiveKit API)
func (rm *RoomManager) stopEgress(egressID, roomName, connectionID string) {
	logger.Base().Info("🛑 Stopping egress", zap.String("egress_id", egressID))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Call LiveKit API to stop recording
	info, err := rm.egressClient.StopEgress(ctx, &livekit.StopEgressRequest{
		EgressId: egressID,
	})

	if err != nil {
		logger.Base().Error("Failed to stop egress")
		logger.Base().Error("📊 egress_ended", zap.String("egress_id", egressID), zap.String("room_name", roomName), zap.String("connection_id", connectionID), zap.String("status", "error"))
		return
	}

	logger.Base().Info("Egress stopped", zap.String("status", info.Status.String()), zap.String("room", roomName), zap.String("connection_id", connectionID), zap.String("egress_id", egressID))

	// Trigger recording_available event after recording completion
	if info.Status == livekit.EgressStatus_EGRESS_COMPLETE {
		logger.Base().Info("💾 Recording completed and available")
		logger.Base().Info("📊 recording_available", zap.String("egress_id", egressID), zap.String("room_name", roomName), zap.String("connection_id", connectionID))
	}
}

// StartCleanupRoutine starts a background routine to clean up expired rooms
func (rm *RoomManager) StartCleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	logger.Base().Info("Started cleanup routine")

	for {
		select {
		case <-ctx.Done():
			logger.Base().Info("🛑 Cleanup routine stopped")
			return
		case <-ticker.C:
			rm.cleanupExpiredRooms(30 * time.Minute)
		}
	}
}

// cleanupExpiredRooms removes rooms that have been inactive for a duration
func (rm *RoomManager) cleanupExpiredRooms(duration time.Duration) {
	// Collect expired room IDs while holding the lock
	rm.mutex.RLock()
	now := time.Now()
	var expiredIDs []string

	for connectionID, room := range rm.rooms {
		if now.Sub(room.CreatedAt) > duration {
			expiredIDs = append(expiredIDs, connectionID)
		}
	}
	rm.mutex.RUnlock()

	// Cleanup rooms after releasing the lock to avoid deadlock
	for _, id := range expiredIDs {
		logger.Base().Info("🗑 Cleaning up expired room", zap.String("connection_id", id))
		rm.CleanupRoom(id)
	}
}
