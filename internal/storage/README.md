# Audio Storage Module

The Audio Storage module provides comprehensive audio caching and processing capabilities for the WhatsApp Call service. It handles real-time audio data from WhatsApp input and OpenAI output streams, processes them into synchronized stereo audio files, and stores them in various backends.

## Features

- **Real-time Audio Caching**: Asynchronously caches RTP packets with precise timestamps
- **Stereo Audio Generation**: Merges left (WhatsApp input) and right (OpenAI output) channels
- **Multiple Storage Backends**: Supports local filesystem and Google Cloud Storage (GCS)
- **Automatic Cleanup**: Periodic cleanup of old cached data (30-minute retention)
- **FFmpeg Integration**: Uses FFmpeg for high-quality audio merging
- **Reference Counting**: Ensures proper cleanup when both input/output streams finish

## Architecture

### Core Components

```
AudioCacheService
├── Audio Chunks Management
│   ├── WhatsApp Input Chunks
│   └── OpenAI Output Chunks
├── RTP Timestamp Processing
├── Ogg Opus Encoding
├── FFmpeg Stereo Merging
├── Storage Backend (Local/GCS)
└── Cleanup Timer
```

### Data Flow

1. **Audio Reception**: RTP packets are cached with reception timestamps
2. **Stream Completion**: Reference counting triggers upload when both streams finish
3. **Timestamp Normalization**: RTP timestamps are normalized relative to earliest packet
4. **Channel Generation**: Separate left/right channel Ogg Opus files are created
5. **Stereo Merging**: FFmpeg merges channels into synchronized stereo audio
6. **Storage Upload**: Final merged file is uploaded to configured backend
7. **Cleanup**: Temporary files and old cache data are automatically removed

## Configuration

### Environment Variables

```bash
# Storage Configuration
AUDIO_CACHE_ENABLED=true
AUDIO_STORAGE_TYPE=local  # or "gcs"
AUDIO_STORAGE_PATH=/path/to/storage  # Local path or GCS bucket name

# GCS Configuration (if using GCS)
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
```

### Storage Types

#### Local Storage
```go
storageType := StorageTypeLocal
storagePath := "/var/audio/cache"
```

#### Google Cloud Storage
```go
storageType := StorageTypeGCS
storagePath := "my-audio-bucket"
```

## Usage

### Initialization

```go
import "github.com/your-org/whatsappcall/storage"

// Initialize audio cache service
cache, err := storage.NewAudioCacheService(
    ctx,
    storage.StorageTypeLocal,
    "/tmp/audio-cache",
)
if err != nil {
    log.Fatal(err)
}
defer cache.Close()
```

### Caching Audio Data

```go
// Cache WhatsApp input audio
cache.CacheAudioRTP(
    connectionID,
    storage.AudioTypeWhatsAppInput,
    storage.AudioFormatOpus,
    rtpPacket,
)

// Cache OpenAI output audio
cache.CacheAudioRTP(
    connectionID,
    storage.AudioTypeAIOutput,
    storage.AudioFormatOpus,
    rtpPacket,
)
```

### Connection Cleanup

```go
// Signal stream completion (call twice for input + output)
cache.CleanupConnection(connectionID)
```

## File Structure

### Generated Files

```
/tmp/whatsappcall/
├── {storagePath}/
│   ├── conversation_{connectionID}_left.opus    # WhatsApp input
│   ├── conversation_{connectionID}_right.opus  # OpenAI output
│   └── conversation_{connectionID}_merged.opus  # Final stereo
```

### File Naming Convention

- **Left Channel**: `conversation_{connectionID}_left.opus`
- **Right Channel**: `conversation_{connectionID}_right.opus`
- **Merged File**: `conversation_{connectionID}_merged.opus`

## Audio Processing

### RTP Timestamp Handling

The service normalizes RTP timestamps to ensure proper synchronization:

```go
// Calculate relative timestamp from earliest packet
relativeTime := chunk.timestamp.Sub(earliestTime)
rtpTimestamp := uint32(relativeTime.Milliseconds()) * 48
```

### FFmpeg Command

```bash
ffmpeg -i left.opus -i right.opus \
-filter_complex "[0:a]aresample=48000,pan=mono|c0=c0[left]; [1:a]aresample=48000,pan=mono|c0=c0[right]; [left][right]join=inputs=2:channel_layout=stereo[aout]" \
-map "[aout]" -c:a libopus output.opus
```

### Audio Specifications

- **Sample Rate**: 48kHz
- **Format**: Opus codec
- **Channels**: Stereo (left: WhatsApp input, right: OpenAI output)
- **Duration**: Actual conversation length + 2s buffer

## Memory Management

### Cache Cleanup

- **Retention Period**: 30 minutes
- **Cleanup Frequency**: Every 10 minutes
- **Cleanup Scope**: Entire connection data (chunks, ref counts, timestamps)

### Reference Counting

Each connection starts with 2 references (input + output streams):
- Decrements on each `CleanupConnection()` call
- Triggers upload when count reaches 0
- Prevents premature cleanup during active streams

## Error Handling

### Common Error Scenarios

1. **Empty Audio Data**
   ```
   ⚠️ WARNING: Channel data is empty for {connectionID}
   ```

2. **FFmpeg Failures**
   ```
   ❌ FFmpeg merge failed: exit status 1
   ```

3. **Storage Errors**
   ```
   ❌ Failed to upload to GCS: permission denied
   ```

### Recovery Mechanisms

- **Graceful Degradation**: Continues operation even if some uploads fail
- **Automatic Retry**: Built-in retry logic for transient failures
- **Resource Cleanup**: Ensures temporary files are always cleaned up
- **Memory Protection**: Prevents memory leaks through automatic cleanup

## Performance Considerations

### Optimization Features

- **Asynchronous Processing**: Non-blocking audio caching
- **Batch Operations**: Efficient bulk data processing
- **Memory Pooling**: Reuses RTP packet structures
- **Lazy Cleanup**: Only cleans up when necessary

### Resource Limits

- **Timeout Protection**: 30-second FFmpeg command timeout
- **Memory Bounds**: Automatic cleanup prevents unbounded growth
- **Concurrent Safety**: Thread-safe operations with proper locking

## Monitoring and Logging

### Key Metrics

- **Cache Hit Rate**: Percentage of successful audio processing
- **Storage Utilization**: Disk/cloud storage usage
- **Processing Latency**: Time from stream completion to file upload
- **Error Rates**: Failed operations per connection

### Log Messages

```
📊 Audio chunks: WhatsApp=150, OpenAI=200, duration=2m30s
🕐 Total conversation duration: 2m32s
✅ FFmpeg merge completed successfully
🗑️ Cleaned up 5 old cached connections
```

## Development

### Testing

```go
// Create test audio chunks
chunks := []*audioChunk{
    {rtpPacket: testPacket, audioType: AudioTypeWhatsAppInput, timestamp: time.Now()},
}

// Test Ogg Opus file creation
data, err := cache.CreateOggOpusFile(chunks)
if err != nil {
    t.Fatal(err)
}
```

### Debugging

Enable debug logging to trace audio processing:

```go
log.SetLevel(log.DebugLevel)
```

## Dependencies

### Required Packages

- `github.com/pion/rtp`: RTP packet handling
- `cloud.google.com/go/storage`: GCS integration
- `os/exec`: FFmpeg command execution

### System Requirements

- **FFmpeg**: For audio merging (installed in Docker)
- **Go 1.24+**: Runtime environment
- **Storage**: Local filesystem or GCS bucket access

## Troubleshooting

### Common Issues

1. **FFmpeg Not Found**
   - Ensure FFmpeg is installed in Docker container
   - Check PATH environment variable

2. **Permission Errors**
   - Verify storage path permissions
   - Check GCS service account credentials

3. **Memory Issues**
   - Monitor cache cleanup frequency
   - Adjust retention period if needed

4. **Audio Sync Issues**
   - Verify RTP timestamp accuracy
   - Check FFmpeg filter configuration

### Debug Commands

```bash
# Check FFmpeg installation
docker exec -it container ffmpeg -version

# Monitor storage usage
docker exec -it container du -sh /tmp/whatsappcall

# View recent logs
docker logs --tail 100 container
```

## Contributing

When contributing to the storage module:

1. **Follow Go Conventions**: Use standard Go formatting and naming
2. **Add Tests**: Include unit tests for new functionality
3. **Update Documentation**: Keep README current with changes
4. **Consider Performance**: Optimize for memory and CPU usage
5. **Handle Errors**: Implement proper error handling and logging

## License

This module is part of the Astra Gateway project and follows the same licensing terms.
