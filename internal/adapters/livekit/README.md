# LiveKit Integration Module

## Overview

This module provides LiveKit integration for the WhatsApp Call Gateway, allowing clients to connect via LiveKit instead of direct WebRTC SDP exchange.

## Architecture

```
Browser/Mobile Client
    ↓ LiveKit Client SDK
LiveKit Server (SFU)
    ↓ LiveKit Server SDK (Go)
Our Gateway (this module)
    ↓ Reuses existing model integration
AI Realtime API
```

## Key Components

### 1. Configuration (`config.go`)
- `LiveKitConfig`: Holds LiveKit server URL, API key, and secret
- Validation and initialization logic

### 2. Connection Management (`connection.go`)
- `LiveKitConnection`: Represents a single LiveKit session
- Manages connection state, model client, and audio tracks
- Thread-safe operations with Mutex

### 3. Room Manager (`room_manager.go`)
- `RoomManager`: Core manager for LiveKit rooms
- Creates rooms and generates access tokens
- Joins rooms as bot to handle server-side logic
- Manages connection lifecycle and cleanup
- Integrates with model handler (reuses existing logic)

### 4. Audio Processing (`audio_processor.go`)
- `AudioProcessor`: Handles audio encoding/decoding
- Opus decoder for LiveKit → model (48kHz mono)
- Opus encoder for model → LiveKit (48kHz mono, 32kbps)
- Reuses audio processing patterns from `webrtc_processor.go`

### 5. Track Handler (`track_handler.go`)
- `TrackHandler`: Manages LiveKit track subscriptions
- Handles audio tracks from remote participants
- Forwards audio to the model
- Publishes model audio back to LiveKit

## Usage

### 1. Enable LiveKit in Configuration

Set environment variables:
```bash
export LIVEKIT_ENABLED=true
export LIVEKIT_SERVER_URL=wss://your-livekit-server.com
export LIVEKIT_API_KEY=your-api-key
export LIVEKIT_API_SECRET=your-api-secret
```

### 2. Create a Room

**POST** `/livekit/create-room`

Request:
```json
{
  "participantName": "user-123",
  "agentId": "agent-456",
  "voiceLanguage": "en",
  "tenantId": "tenant-789"
}
```

Response:
```json
{
  "connectionId": "livekit-1699999999999",
  "roomName": "astra-livekit-1699999999999",
  "accessToken": "eyJhbGc...",
  "serverUrl": "wss://your-livekit-server.com",
  "status": "created"
}
```

### 3. Client Side (JavaScript Example)

```javascript
import { Room, RoomEvent } from 'livekit-client';

// Create room instance
const room = new Room();

// Connect to room using access token
await room.connect(serverUrl, accessToken);

// Publish microphone audio
const audioTrack = await createLocalAudioTrack();
await room.localParticipant.publishTrack(audioTrack);

// Listen for audio from the model
room.on(RoomEvent.TrackSubscribed, (track, publication, participant) => {
  if (track.kind === 'audio') {
    const audioElement = track.attach();
    document.body.appendChild(audioElement);
  }
});
```

### 4. End Call

**POST** `/livekit/end-call`

Request:
```json
{
  "connectionId": "livekit-1699999999999"
}
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/livekit/create-room` | Create new room and get access token |
| POST | `/livekit/join-room` | Join existing room (future) |
| POST | `/livekit/end-call` | End call and cleanup resources |
| GET | `/livekit/connection-status/:id` | Get connection status |
| GET | `/livekit/stats` | Get LiveKit statistics |

## Audio Flow

### Client → AI/model
1. Client publishes audio track (Opus, 48kHz mono)
2. LiveKit server forwards to our bot participant
3. `TrackHandler.HandleTrackSubscribed()` receives track
4. `AudioProcessor.ProcessIncomingAudio()` decodes Opus to PCM16
5. PCM16 sent to the model via existing `WebRTCClient.SendAudio()`

### Model → Client
1. Model sends audio (Opus, 48kHz mono)
2. Audio is written to WAOutputTrack and published to LiveKit
3. Audio published to LiveKit room
4. LiveKit server forwards to client

## Integration with Existing Code

### Reused Components
- ✅ **Model Handler** (e.g., OpenAI) (`internal/core/model/openai/handler.go`)
  - `InitializeConnectionWithLanguage()` - Establishes model connection
  - Agent configuration and prompt generation
  
- ✅ **Model WebRTC Client** (`internal/adapters/webrtc/client.go`)
  - `SendAudio()` - Sends PCM16 audio to the model
  - Event handling and data channel
  
- ✅ **Audio Processing Logic** (patterns from `webrtc_processor.go`)
  - Opus encoding/decoding
  - Audio buffering and forwarding

### No Changes to Existing Code
- ❌ **No modifications** to WhatsApp webhook handlers
- ❌ **No modifications** to existing WebRTC processor
- ❌ **No modifications** to model integration
- ✅ **Only additions** in new `livekit/` module

## Configuration

Add to `whatsapp_config.go`:

```go
type WhatsAppCallConfig struct {
    // ... existing fields ...
    
    // LiveKit configuration (NEW)
    LiveKitEnabled    bool   `json:"livekit_enabled"`
    LiveKitServerURL  string `json:"livekit_server_url"`
    LiveKitAPIKey     string `json:"livekit_api_key"`
    LiveKitAPISecret  string `json:"livekit_api_secret"`
}
```

## Testing

### Manual Testing

1. **Start LiveKit Server**
   ```bash
   docker run -d --name livekit \
     -p 7880:7880 -p 7881:7881 \
     -e LIVEKIT_API_KEY=test-key \
     -e LIVEKIT_API_SECRET=test-secret \
     livekit/livekit-server:latest
   ```

2. **Start Application**
   ```bash
   export LIVEKIT_ENABLED=true
   export LIVEKIT_SERVER_URL=ws://localhost:7880
   export LIVEKIT_API_KEY=test-key
   export LIVEKIT_API_SECRET=test-secret
   go run cmd/whatsappcall/main.go
   ```

3. **Create Room**
   ```bash
   curl -X POST http://localhost:8082/livekit/create-room \
     -H "Content-Type: application/json" \
     -d '{
       "participantName": "test-user",
       "agentId": "agent-1",
       "voiceLanguage": "en"
     }'
   ```

4. **Use LiveKit Client** (see client example above)

### Unit Tests

```bash
cd whatsappcall/livekit
go test -v ./...
```

## Troubleshooting

### Connection Issues
- Check LiveKit server is running: `curl http://localhost:7880/`
- Verify API key and secret are correct
- Check firewall rules for ports 7880, 7881

### Audio Issues
- Verify Opus codec support
- Check audio sample rate (must be 48kHz)
- Monitor logs for `[LiveKit]` prefix
- Ensure audio tracks are properly subscribed

### Model Integration Notes
- Verify model provider credentials are set (e.g., OpenAI key if using OpenAI)
- Check ephemeral token generation
- Monitor model connection logs
- Ensure agent configuration is valid

## Performance Considerations

- **Connection Pooling**: Reuses model connections efficiently
- **Audio Buffering**: Minimal buffering for low latency
- **Cleanup**: Automatic cleanup of expired connections (30min timeout)
- **Scalability**: LiveKit SFU handles media routing, reducing server load

## Future Enhancements

- [ ] Video track support
- [ ] Multiple participants per room (conference mode)
- [ ] Recording via LiveKit Egress
- [ ] Real-time transcription
- [ ] Custom audio preprocessing (noise suppression, echo cancellation)
- [ ] Metrics and monitoring integration

## References

- [LiveKit Documentation](https://docs.livekit.io/)
- [LiveKit Go SDK](https://github.com/livekit/server-sdk-go)
- [LiveKit Client SDK](https://github.com/livekit/client-sdk-js)
- [OpenAI Realtime API](https://platform.openai.com/docs/guides/realtime)

## Support

For issues or questions, refer to:
- Project documentation in `whatsappcall/doc/`
- LiveKit integration plan: `LIVEKIT_INTEGRATION_PLAN.md`
- Implementation checklist: `LIVEKIT_IMPLEMENTATION_CHECKLIST.md`

