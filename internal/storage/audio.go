package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClareAI/astra-voice-service/pkg/gcs"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/pion/rtp"
	"go.uber.org/zap"
)

// AudioType represents the type of audio being stored
type AudioType string

const sampleRate = 48000
const delayRtpTimestamp = 960 * 50
const tmpStoragePath = "/tmp"

const (
	AudioTypeWhatsAppInput AudioType = "whatsapp_input"
	AudioTypeAIOutput      AudioType = "ai_output"
)

// AudioFormat represents the audio format
type AudioFormat string

const (
	AudioFormatOpus  AudioFormat = "opus"
	AudioFormatPCM16 AudioFormat = "pcm16"
)

// StorageType represents the type of storage backend
type StorageType string

const (
	StorageTypeLocal StorageType = "local"
	StorageTypeGCS   StorageType = "gcs"
)

// AudioCacheService handles asynchronous audio caching to local or GCS storage
type AudioCacheService struct {
	storageType StorageType
	gcsClient   *gcs.GCSClient
	enabled     bool
	storagePath string // Local directory or GCS bucket name
	ctx         context.Context
	cancel      context.CancelFunc

	// Audio processing configuration
	leftChannelVolume  float64 // Volume multiplier for left channel (WhatsApp input)
	rightChannelVolume float64 // Volume multiplier for right channel (model output)

	// Connection-based chunks (merged mode - all audio in one file)
	chunks map[string](map[string][]*audioChunk) // connectionID -> ordered chunks
	mu     sync.RWMutex

	// Reference counting for cleanup (ensure both input and output streams finish)
	refCounts sync.Map // map[string]*int32 - lock-free atomic operations

	// Base timestamps for each audio type per connection
	baseTimestamps sync.Map // map[string]*connectionBaseTimestamps

	// Cleanup timer for old temporary files
	cleanupTicker *time.Ticker

	// Connection ID to Conversation ID mapping
	connectionToConversation sync.Map // map[string]string
}

// audioChunk represents a chunk of audio with timestamp for ordering
type audioChunk struct {
	rtpPacket *rtp.Packet
	audioType AudioType
	timestamp time.Time
}

// NewAudioCacheService creates a new audio cache service
func NewAudioCacheService(ctx context.Context, storageType StorageType, storagePath string) (*AudioCacheService, error) {
	return NewAudioCacheServiceWithVolume(ctx, storageType, storagePath, 2.0, 1.0)
}

// NewAudioCacheServiceWithVolume creates a new audio cache service with volume configuration
func NewAudioCacheServiceWithVolume(ctx context.Context, storageType StorageType, storagePath string, leftVolume, rightVolume float64) (*AudioCacheService, error) {
	if storagePath == "" {
		logger.Base().Info("Audio cache disabled (no storage path configured)")
		return &AudioCacheService{enabled: false}, nil
	}

	serviceCtx, cancel := context.WithCancel(ctx)

	service := &AudioCacheService{
		storageType:        storageType,
		enabled:            true,
		storagePath:        storagePath,
		ctx:                serviceCtx,
		cancel:             cancel,
		leftChannelVolume:  leftVolume,
		rightChannelVolume: rightVolume,
		chunks:             make(map[string]map[string][]*audioChunk),
		// connectionToConversation is sync.Map, no initialization needed
	}

	// Initialize storage backend
	switch storageType {
	case StorageTypeGCS:
		// Create GCS client
		gcsClient, err := gcs.NewGCSClient(ctx, storagePath)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create GCS client: %v", err)
		}
		service.gcsClient = gcsClient
		logger.Base().Info("☁ Audio cache service started (GCS bucket: )", zap.String("storagepath", storagePath))

	case StorageTypeLocal:
		// Create local storage directory if it doesn't exist
		storagePath = filepath.Join(tmpStoragePath, storagePath)
		if err := os.MkdirAll(storagePath, 0755); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create local storage directory: %v", err)
		}
		logger.Base().Info("💾 Audio cache service started (Local path: )", zap.String("storagepath", storagePath))

	default:
		cancel()
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}

	logger.Base().Info("Audio cache service started", zap.String("storage_type", string(storageType)), zap.String("mode", "merged"))

	// Start cleanup timer
	service.startCleanupTimer()

	return service, nil
}

// SetConversationID sets the conversation ID for a connection
func (s *AudioCacheService) SetConversationID(connectionID, conversationID string) {
	s.connectionToConversation.Store(connectionID, conversationID)
	logger.Base().Info("Set conversation ID for connection", zap.String("conversation_id", conversationID), zap.String("connection_id", connectionID))
}

// GetConversationID gets the conversation ID for a connection
func (s *AudioCacheService) GetConversationID(connectionID string) string {
	if conversationID, exists := s.connectionToConversation.Load(connectionID); exists {
		return conversationID.(string)
	}
	return connectionID // fallback to connectionID
}

// CacheAudioRTP asynchronously caches RTP packet data with timestamp conversion
func (s *AudioCacheService) CacheAudioRTP(connectionID string, audioType AudioType, format AudioFormat, rtpPacket *rtp.Packet) {
	if !s.enabled || rtpPacket == nil || len(rtpPacket.Payload) == 0 {
		return
	}

	// Create a copy of the RTP packet for timestamp conversion
	normalizedPacket := *rtpPacket

	// Convert timestamps to WhatsApp standard
	// normalizedPacket.Timestamp = s.convertTimestamp(connectionID, audioType, rtpPacket.Timestamp) // normalizedPacket.Timestamp = uint32(time.Now().UnixNano() * 48 / 1000000)
	// Create audio chunk with precise timestamp
	// Use actual reception time for accurate timing
	chunk := &audioChunk{
		rtpPacket: &normalizedPacket,
		audioType: audioType,
		timestamp: time.Now().UTC(), // 使用实际接收时间确保时序正确
	}

	// Append to chunks list for this connection
	s.mu.Lock()
	if _, exists := s.chunks[connectionID]; !exists {
		s.chunks[connectionID] = make(map[string][]*audioChunk)
	}
	s.chunks[connectionID][string(audioType)] = append(s.chunks[connectionID][string(audioType)], chunk)
	s.mu.Unlock()
}

// CleanupConnection decrements reference count and uploads when both streams finish
func (s *AudioCacheService) CleanupConnection(connectionID string) {
	if !s.enabled {
		return
	}

	// Get or create reference count (start with 2 for input + output streams)
	refCountPtr, _ := s.refCounts.LoadOrStore(connectionID, func() *int32 {
		count := int32(2) // Start with 2 references (input + output streams)
		return &count
	}())
	refCount := refCountPtr.(*int32)

	// Decrement reference count
	newCount := atomic.AddInt32(refCount, -1)
	logger.Base().Info("Ref count decremented", zap.Int32("new_count", newCount), zap.String("connection_id", connectionID))

	// If this was the last reference, queue for upload
	if newCount == 0 {
		logger.Base().Info("Queueing", zap.String("connection_id", connectionID))
		go s.uploadAudio(connectionID)
	} else if newCount < 0 {
		logger.Base().Warn("Negative ref count", zap.Int32("new_count", newCount), zap.String("connection_id", connectionID))
	}
}

// setRTPTimestamps sets timestamps for RTP packets based on earliest time
func (s *AudioCacheService) setRTPTimestamps(chunks []*audioChunk, earliestTime time.Time, delay uint32) []*rtp.Packet {
	if len(chunks) == 0 {
		return nil
	}

	rtpPackets := make([]*rtp.Packet, len(chunks))
	firstRTPTimestamp := chunks[0].rtpPacket.Timestamp
	firstTimestamp := uint32(chunks[0].timestamp.Sub(earliestTime).Milliseconds()) * 48

	for i, chunk := range chunks {
		pkt := *chunk.rtpPacket
		pkt.SequenceNumber = uint16(i)
		if i == 0 {
			pkt.Timestamp = firstTimestamp + delay
		} else {
			pkt.Timestamp = uint32(pkt.Timestamp - firstRTPTimestamp + firstTimestamp + delay)
		}
		rtpPackets[i] = &pkt
	}

	return rtpPackets
}

// cleanupOldCache removes cached chunks older than 30 minutes
func (s *AudioCacheService) cleanupOldCache() {
	if !s.enabled {
		return
	}

	cutoffTime := time.Now().Add(-30 * time.Minute)

	s.mu.Lock()
	defer s.mu.Unlock()

	var cleanedConnections []string

	for connectionID, chunks := range s.chunks {
		// Check if any chunk in this connection is older than cutoff time
		shouldCleanup := false
		for _, audioTypeChunks := range chunks {
			for _, chunk := range audioTypeChunks {
				if chunk.timestamp.Before(cutoffTime) {
					shouldCleanup = true
					break
				}
			}
			if shouldCleanup {
				break
			}
		}

		if shouldCleanup {
			cleanedConnections = append(cleanedConnections, connectionID)
			// Remove chunks, reference count, base timestamps, and conversation mapping
			delete(s.chunks, connectionID)
			s.refCounts.Delete(connectionID)
			s.baseTimestamps.Delete(connectionID)
			s.connectionToConversation.Delete(connectionID)
		}
	}

	if len(cleanedConnections) > 0 {
		logger.Base().Info("Cleaned old connections", zap.Int("count", len(cleanedConnections)))
	}
}

// startCleanupTimer starts the periodic cleanup timer
func (s *AudioCacheService) startCleanupTimer() {
	if !s.enabled {
		return
	}

	// Run cleanup every 10 minutes
	s.cleanupTicker = time.NewTicker(10 * time.Minute)

	go func() {
		defer s.cleanupTicker.Stop()

		for {
			select {
			case <-s.cleanupTicker.C:
				s.cleanupOldCache()
			case <-s.ctx.Done():
				logger.Base().Info("🛑 Cleanup timer stopped")
				return
			}
		}
	}()

	logger.Base().Info("🧹 Started cleanup timer (10min interval)")
}

// mergeAudioWithFFmpeg merges left and right channel audio files using ffmpeg
func (s *AudioCacheService) mergeAudioWithFFmpeg(leftPath, rightPath, outputPath string) error {
	// Ensure output directory exists
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %v", outputDir, err)
	}

	// Build volume filter strings
	leftVolumeFilter := ""
	rightVolumeFilter := ""

	if s.leftChannelVolume != 1.0 {
		leftVolumeFilter = fmt.Sprintf(",volume=%.2f", s.leftChannelVolume)
	}
	if s.rightChannelVolume != 1.0 {
		rightVolumeFilter = fmt.Sprintf(",volume=%.2f", s.rightChannelVolume)
	}

	filterComplex := fmt.Sprintf("[0:a]aresample=48000,pan=mono|c0=c0%s[left]; [1:a]aresample=48000,pan=mono|c0=c0%s[right]; [left][right]join=inputs=2:channel_layout=stereo[aout]",
		leftVolumeFilter, rightVolumeFilter)

	cmd := exec.Command("ffmpeg",
		"-i", leftPath,
		"-i", rightPath,
		"-filter_complex", filterComplex,
		"-map", "[aout]",
		"-c:a", "libopus",
		"-y", // Overwrite output file
		outputPath)

	// Set timeout for ffmpeg command
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd = exec.CommandContext(ctx, cmd.Args[0], cmd.Args[1:]...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Base().Error("FFmpeg merge failed", zap.String("output", string(output)), zap.Error(err))
		return fmt.Errorf("ffmpeg merge failed: %v", err)
	}

	logger.Base().Info("FFmpeg completed", zap.String("outputpath", outputPath))
	return nil
}

// uploadAudio prepares and uploads audio for a connection
func (s *AudioCacheService) uploadAudio(connectionID string) {
	// Extract data while holding the lock (minimize lock duration)
	s.mu.Lock()
	chunks, exists := s.chunks[connectionID]
	if !exists || len(chunks) == 0 {
		s.mu.Unlock()
		return
	}

	// Make copies of the chunk slices to avoid holding the lock during processing
	whatsappChunks := make([]*audioChunk, len(chunks[string(AudioTypeWhatsAppInput)]))
	copy(whatsappChunks, chunks[string(AudioTypeWhatsAppInput)])

	aiChunks := make([]*audioChunk, len(chunks[string(AudioTypeAIOutput)]))
	copy(aiChunks, chunks[string(AudioTypeAIOutput)])

	// Get conversation ID while holding lock
	conversationID := s.GetConversationID(connectionID)

	// Clean up data immediately after extracting
	delete(s.chunks, connectionID)
	s.refCounts.Delete(connectionID)
	s.baseTimestamps.Delete(connectionID)
	s.connectionToConversation.Delete(connectionID)
	s.mu.Unlock()

	// Process audio without holding the lock
	// 找到最早的接收时间作为基准
	var earliestTime, latestTime time.Time
	allChunks := append(whatsappChunks, aiChunks...)

	for _, chunk := range allChunks {
		if earliestTime.IsZero() || chunk.timestamp.Before(earliestTime) {
			earliestTime = chunk.timestamp
		}
		if chunk.timestamp.After(latestTime) {
			latestTime = chunk.timestamp
		}
	}

	logger.Base().Info("Audio statistics", zap.Int("whatsapp_chunks", len(whatsappChunks)), zap.Int("ai_chunks", len(aiChunks)), zap.Duration("duration", latestTime.Sub(earliestTime)))
	logger.Base().Info("Using conversation ID for connection", zap.String("conversation_id", conversationID), zap.String("connection_id", connectionID))

	// 创建RTP包并设置统一时间戳
	whatsappRTPPackets := s.setRTPTimestamps(whatsappChunks, earliestTime, 0)
	aiRTPPackets := s.setRTPTimestamps(aiChunks, earliestTime, delayRtpTimestamp)
	// Create proper Ogg Opus container using Pion OGG writer
	encoder := NewOggOpusEncoder()

	// 创建分离的左右声道文件：确保时序正确
	// 左声道文件：WhatsApp输入 + 静音填充
	// 右声道文件：静音填充 + 模型输出

	// 计算总时长
	totalDuration := latestTime.Sub(earliestTime) + 2000*time.Millisecond // 添加2s缓冲
	if totalDuration < 100*time.Millisecond {
		totalDuration = 100 * time.Millisecond // 最小时长100ms
	}

	logger.Base().Info("Total duration", zap.Duration("duration", totalDuration))

	// 创建左声道文件（WhatsApp输入 + 静音填充）
	leftChannelData, err := encoder.CreateMonoOggOpusFile(whatsappRTPPackets, totalDuration)
	if err != nil {
		logger.Base().Error("Left channel failed")
		return
	}

	// 创建右声道文件（静音填充 + 模型输出）
	rightChannelData, err := encoder.CreateMonoOggOpusFile(aiRTPPackets, totalDuration)
	if err != nil {
		logger.Base().Error("Right channel failed")
		return
	}

	// Check if we have valid data before proceeding
	if len(leftChannelData) == 0 || len(rightChannelData) == 0 {
		logger.Base().Warn("Empty data", zap.String("connection_id", connectionID))
		return
	}

	logger.Base().Info("Uploading audio", zap.String("connection_id", connectionID), zap.Int("left_bytes", len(leftChannelData)), zap.Int("right_bytes", len(rightChannelData)), zap.Int("total_bytes", len(leftChannelData)+len(rightChannelData)))

	// Generate file paths for separate channels and merged output
	// Format: whatsappaudio/conversation_{connectionID}_left.opus
	// Format: whatsappaudio/conversation_{connectionID}_right.opus
	// Format: whatsappaudio/conversation_{connectionID}_merged.opus
	var leftPath, rightPath, mergedPath string
	defer func() {
		if leftPath != "" {
			os.Remove(leftPath)
		}
		if rightPath != "" {
			os.Remove(rightPath)
		}
		if mergedPath != "" && s.storageType != StorageTypeLocal {
			os.Remove(mergedPath)
		}
	}()
	leftRelativePath := fmt.Sprintf("whatsappcall/conversation_%s_left.opus", conversationID)
	rightRelativePath := fmt.Sprintf("whatsappcall/conversation_%s_right.opus", conversationID)
	mergedRelativePath := fmt.Sprintf("whatsappcall/conversation_%s_merged.opus", conversationID)
	mergedPath = filepath.Join(tmpStoragePath, s.storagePath, mergedRelativePath)

	// Upload left and right channel files to temp directory
	leftPath, err = s.uploadToLocal(conversationID, leftChannelData, leftRelativePath)
	if err != nil {
		logger.Base().Error("Failed to upload left channel audio to local")
		return
	}
	rightPath, err = s.uploadToLocal(conversationID, rightChannelData, rightRelativePath)
	if err != nil {
		logger.Base().Error("Failed to upload right channel audio to local")
		return
	}

	// Merge left and right channels using ffmpeg
	err = s.mergeAudioWithFFmpeg(leftPath, rightPath, mergedPath)
	if err != nil {
		logger.Base().Error("Failed to merge audio channels")
		return
	}

	// Upload merged file based on storage type
	switch s.storageType {
	case StorageTypeGCS:
		// Read merged file and upload to GCS
		mergedData, err := os.ReadFile(mergedPath)
		if err != nil {
			logger.Base().Error("Failed to read merged audio file")
			return
		}
		s.uploadToGCS(conversationID, mergedData, mergedRelativePath)
	default:
		return
	}
}

// uploadToGCS uploads audio to GCS using the pkg GCS client
func (s *AudioCacheService) uploadToGCS(conversationID string, data []byte, objectPath string) {
	logger.Base().Info("💾 Uploading to GCS", zap.String("conversationid", conversationID))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	reader := bytes.NewReader(data)
	url, err := s.gcsClient.Upload(ctx, objectPath, reader)
	if err != nil {
		logger.Base().Error("Failed to upload channel audio to GCS")
		return
	}

	logger.Base().Info("Uploaded to GCS", zap.String("url", url), zap.Int("bytes", len(data)), zap.String("conversation_id", conversationID))
}

// uploadToLocal uploads audio to local filesystem
func (s *AudioCacheService) uploadToLocal(conversationID string, data []byte, relativePath string) (string, error) {
	fullPath := filepath.Join(tmpStoragePath, s.storagePath, relativePath)
	logger.Base().Info("💾 Writing to", zap.String("conversationid", conversationID), zap.String("fullpath", fullPath))

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Base().Error("Failed to create directory", zap.String("dir", dir))
		return "", err
	}

	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		logger.Base().Error("Failed to write channel audio to local file")
		return "", err
	}

	logger.Base().Info("Saved to local file", zap.String("path", fullPath), zap.Int("bytes", len(data)), zap.String("conversation_id", conversationID))
	return fullPath, nil
}

// CreateOggOpusFile creates a proper Ogg Opus file from audio chunks (public method for testing)
func (s *AudioCacheService) CreateOggOpusFile(chunks []*audioChunk) ([]byte, error) {
	encoder := NewOggOpusEncoder()

	// Extract RTP packets from chunks
	rtpPackets := make([]*rtp.Packet, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.rtpPacket != nil {
			rtpPackets = append(rtpPackets, chunk.rtpPacket)
		}
	}

	return encoder.CreateOggOpusFile(rtpPackets)
}

// Close gracefully shuts down the audio cache service
func (s *AudioCacheService) Close() error {
	if !s.enabled {
		return nil
	}

	logger.Base().Info("🛑 Shutting down audio cache service...")

	// Cancel context to stop all operations
	s.cancel()

	// Upload all pending chunks before cleanup
	s.mu.Lock()
	pendingConnections := len(s.chunks)
	connectionIDs := make([]string, 0, pendingConnections)
	for connectionID := range s.chunks {
		connectionIDs = append(connectionIDs, connectionID)
	}
	s.mu.Unlock()

	// Upload all pending audio data
	if pendingConnections > 0 {
		logger.Base().Info("Uploading pending audio connections before shutdown", zap.Int("pending_connections", pendingConnections))

		for _, connectionID := range connectionIDs {
			// Upload each connection's audio data
			s.uploadAudio(connectionID)
		}

		logger.Base().Info("Uploaded pending audio connections", zap.Int("pending_connections", pendingConnections))
	}

	// Clean up all pending chunks and reference counts
	s.mu.Lock()
	s.chunks = make(map[string]map[string][]*audioChunk) // Clear all chunks
	s.mu.Unlock()

	// Clear all reference counts
	s.refCounts.Range(func(key, value interface{}) bool {
		s.refCounts.Delete(key)
		return true
	})

	// Close GCS client if it exists
	if s.gcsClient != nil {
		if err := s.gcsClient.Close(); err != nil {
			logger.Base().Error("Error closing GCS client")
		}
	}

	logger.Base().Info("Audio cache service shut down", zap.Int("uploaded_pending_connections", pendingConnections))
	return nil
}

// Global audio cache instance
var (
	audioCacheInstance *AudioCacheService
	audioCacheOnce     sync.Once
)

// GetAudioCache returns the global audio cache instance
func GetAudioCache() *AudioCacheService {
	return audioCacheInstance
}

// SetAudioCache sets the global audio cache instance
func SetAudioCache(cache *AudioCacheService) {
	audioCacheOnce.Do(func() {
		audioCacheInstance = cache
	})
}

// InitAudioCache initializes the global audio cache instance
func InitAudioCache(ctx context.Context, enabled bool, storageType StorageType, storagePath string) error {
	return InitAudioCacheWithVolume(ctx, enabled, storageType, storagePath, 1.5, 1.0)
}

// InitAudioCacheWithVolume initializes the global audio cache instance with volume configuration
func InitAudioCacheWithVolume(ctx context.Context, enabled bool, storageType StorageType, storagePath string, leftVolume, rightVolume float64) error {
	if !enabled {
		logger.Base().Info("Audio cache disabled")
		return nil
	}

	cache, err := NewAudioCacheServiceWithVolume(ctx, storageType, storagePath, leftVolume, rightVolume)
	if err != nil {
		return fmt.Errorf("failed to create audio cache service: %v", err)
	}

	SetAudioCache(cache)
	logger.Base().Info("Audio cache initialized with volume", zap.Float64("left_volume", leftVolume), zap.Float64("right_volume", rightVolume))
	return nil
}
