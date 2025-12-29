# WhatsApp Call Gateway

A Go-based gateway that handles WhatsApp voice calls through Wati API and integrates with OpenAI Realtime API for AI-powered conversations.

> 📚 **完整文档**: 查看 [doc/DOCS_INDEX.md](doc/DOCS_INDEX.md) 获取详细的使用指南、功能说明和故障排查。

## Features

- 📞 **Wati Integration**: Handle WhatsApp calls via Wati x Astra platform
- 🎵 **WebRTC Audio**: Real-time audio processing using WebRTC
- 🤖 **OpenAI Realtime API**: AI-powered voice conversations
- 🔐 **Simple Configuration**: Only 2 required API keys (OpenAI + Wati)
- 🎚️ **Audio Processing**: Opus codec with automatic format conversion
- 📊 **Connection Management**: Track and manage active call connections
- 🌐 **Production Ready**: CORS support and comprehensive error handling

## Architecture

The WhatsApp Call Gateway is built on the same architecture as the existing `mediagateway` but uses Wati API for WhatsApp integration:

```
WhatsApp User → Wati API → Webhook → WebRTC Handler → OpenAI Realtime API
                                         ↓
                                    Audio Processing
                                         ↓
                                    Voice AI Response
```

## Prerequisites

1. **Wati Account**: Set up Wati Business account
2. **Wati API Access**: Get Tenant ID and API Key
3. **OpenAI API Key**: For Realtime API access
4. **Public HTTPS Endpoint**: For webhook delivery (ngrok for development)
5. **Go 1.21+**: For building and running the service

## Quick Start

### 1. Configuration

Copy the example environment file:

```bash
cp whatsappcall/example.env .env
```

Edit the configuration with your credentials:

```env
# Required Configuration
OPENAI_API_KEY=your_openai_api_key
WATI_TENANT_ID=your_tenant_id
WATI_API_KEY=your_wati_api_key

# Optional Configuration
PUBLIC_BASE_URL=https://abc123.ngrok.io
BRANDKIT_BASE_URL=https://dev-astra.engagechat.ai  # For AI agent config generation
```

### 2. Run the Gateway

```bash
# From project root
go run cmd/whatsappcall/main.go
```

The server will start on port 8082 by default.

### 3. Configure Wati Webhook

Set your webhook URL in the Wati console:
- Webhook URL: `https://your-domain.com/wati/webhook`

## API Endpoints

### Core Endpoints

- `POST /wati/webhook` - Receive Wati webhook events

### Manual Control (for testing)

- `POST /wati/calls/{callId}/accept` - Manually accept call
- `POST /wati/calls/{callId}/terminate` - Manually terminate call

### Status & Health

- `GET /wati/status` - Service status and connection count
- `GET /wati/health` - Health check endpoint

## Wati API Integration

### Call Management

The gateway integrates with Wati's call management APIs:

- **Accept Call**: Automatically accepts incoming calls with SDP answer
- **Terminate Call**: Handles call termination events
- **Webhook Events**: Processes `call.start` and `call.end` events

### Webhook Events

Handles the following Wati webhook events:
- `call.start` - Incoming call with WebRTC SDP offer
- `call.end` - Call termination

## WebRTC Implementation

The gateway processes WebRTC signaling for voice calls:

### SDP Handling
- Parses incoming WebRTC offers from Wati
- Generates appropriate SDP answers
- Supports G.711 μ-law audio codec

### Audio Processing
- Receives audio via WebRTC
- Converts to format compatible with OpenAI Realtime API
- Streams AI responses back through WebRTC

## OpenAI Integration

Reuses the proven OpenAI integration from `mediagateway`:

- **Session Management**: Automatic session creation and configuration
- **Audio Streaming**: Real-time audio processing with G.711 μ-law
- **Voice Activity Detection**: Server-side VAD for natural conversations
- **Response Generation**: AI-powered voice responses

## Development

### Project Structure

```
whatsappcall/
├── whatsapp_structs.go      # WhatsApp API data structures
├── wati_webhook_handler.go  # Wati webhook processing logic
├── wati_client.go          # Wati API client
├── openai_webrtc_handler.go # OpenAI WebRTC integration
├── webrtc_processor.go     # Audio processing logic
├── pion_webrtc_template.go # WebRTC SDP handling
├── server.go               # Main server and routing
├── example.env             # Configuration template
└── README.md              # This file
```

### Testing

Use the included test endpoints:

```bash
# Check service status
curl http://localhost:8082/wati/status

# Health check
curl http://localhost:8082/wati/health

# Test webhook
curl -X POST http://localhost:8082/wati/webhook \
  -H "Content-Type: application/json" \
  -d @whatsappcall/wati_test_webhook.json
```

### Debugging

Enable debug logging:

```env
DEBUG=true
```

Monitor webhook events:

```bash
# Follow logs
tail -f /var/log/whatsapp-gateway.log
```

## Production Deployment

### Requirements

1. **HTTPS**: Wati requires HTTPS for webhook delivery
2. **Domain**: Stable domain name for webhook URL
3. **Load Balancing**: For high availability
4. **Monitoring**: Health checks and metrics collection

### Security

- CORS configuration for web integration
- Environment-based configuration management
- Secure API key storage

### Scaling

- Stateless design allows horizontal scaling
- Connection pooling for database operations
- Configurable connection limits

## Troubleshooting

### Common Issues

1. **Webhook Not Received**
   - Check `PUBLIC_BASE_URL` is correctly configured
   - Ensure webhook URL is accessible via HTTPS
   - Verify Wati webhook configuration

2. **Audio Quality Issues**
   - Verify G.711 μ-law codec support
   - Check network connectivity for WebRTC
   - Monitor OpenAI API latency

3. **Connection Timeouts**
   - Increase `WHATSAPP_MAX_CONNECTIONS` if needed
   - Check firewall rules for WebRTC ports
   - Verify STUN/TURN server accessibility

### Logs

Monitor these log patterns:

```
📞 🟢 Call starting: <call_id> from <caller> to <business_number>
🔄 Generating SDP answer for offer: <sdp_size> bytes
✅ Call <call_id> accepted successfully via Wati
🎵 Starting audio processing for Wati call: <connection_id>
📞 🔴 Call ending: <call_id>
✅ Cleaned up connection for ended call: <call_id>
```

## Integration with Existing Services

This gateway is designed to work alongside the existing `mediagateway`:

- **Shared OpenAI Client**: Reuses OpenAI integration code
- **Similar Architecture**: Consistent patterns and structure
- **Configuration**: Compatible environment variable patterns
- **Logging**: Unified logging format and structure

## Contributing

1. Follow the existing code patterns from `mediagateway`
2. Add comprehensive logging for debugging
3. Include error handling for all external API calls
4. Update documentation for new features

## License

Same as the parent project.
