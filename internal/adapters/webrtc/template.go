package webrtc

import (
	"context"
	"strings"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"go.uber.org/zap"
)

// ProcessOfferResult contains the result of processing a WebRTC offer
type ProcessOfferResult struct {
	AnswerSDP    string
	PC           *webrtc.PeerConnection
	OutputTrack  *webrtc.TrackLocalStaticSample // For sending AI audio to WA
	OutputSender *webrtc.RTPSender              // For sending AI audio to WA
	Transceiver  *webrtc.RTPTransceiver         // Single transceiver for both send/recv
}

// ProcessOffer builds a stable one-shot (non-trickle) WebRTC answer for an incoming WhatsApp/Meta SDP offer
// using the expert-recommended minimal approach: NO manual SDP editing, consistent SSRC, proper ordering
// Returns output track for sending AI audio to WA (legacy compatibility)
func ProcessOffer(ctx context.Context, offerSDP string, stunServers []string, turnCredentials []TURNCredentials) (answerSDP string, pc *webrtc.PeerConnection, audioTrack *webrtc.TrackLocalStaticSample, audioSender *webrtc.RTPSender, err error) {
	result, err := ProcessOfferWithTracks(ctx, offerSDP, stunServers, turnCredentials)
	if err != nil {
		return "", nil, nil, nil, err
	}
	return result.AnswerSDP, result.PC, result.OutputTrack, result.OutputSender, nil
}

// ProcessOfferWithTracks builds a WebRTC answer with single transceiver to match WA's single m=audio
func ProcessOfferWithTracks(ctx context.Context, offerSDP string, stunServers []string, turnCredentials []TURNCredentials) (*ProcessOfferResult, error) {
	// 1) Build MediaEngine with ONLY MONO Opus codec to force single channel
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   config.DefaultSampleRate,
			Channels:    config.DefaultChannelsMono, // Force MONO - critical for consistency
			SDPFmtpLine: "stereo=0;sprop-stereo=0;ptime=20;minptime=10;maxaveragebitrate=20000;maxplaybackrate=16000;sprop-maxcapturerate=16000;useinbandfec=1",
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	logger.Base().Info("Registered MONO Opus codec with explicit stereo=0")

	// 2) Force DTLS Active role (client) - fixes passive/active audio issue
	se := webrtc.SettingEngine{}
	se.SetAnsweringDTLSRole(webrtc.DTLSRoleClient) // Answer uses active

	// 3) Build ICE servers configuration from provided STUN and TURN servers
	iceServers := make([]webrtc.ICEServer, 0, len(stunServers)+len(turnCredentials))

	// Add STUN servers
	for _, stunURL := range stunServers {
		iceServers = append(iceServers, webrtc.ICEServer{URLs: []string{stunURL}})
	}
	logger.Base().Info("Using STUN servers", zap.Strings("stun_servers", stunServers))

	// Add TURN servers from Twilio
	for _, cred := range turnCredentials {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:       cred.URLs,
			Username:   cred.Username,
			Credential: cred.Credential,
		})
		logger.Base().Info("Using Twilio TURN servers", zap.Strings("urls", cred.URLs), zap.String("username", cred.Username))
	}

	api := webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(m))

	// Build peer connection configuration
	pcConfig := webrtc.Configuration{
		ICEServers:           iceServers,
		ICECandidatePoolSize: 10, // Pre-gather ICE candidates for faster connection
	}

	// Check if client offer contains only relay candidates (Force TURN mode)
	// If client is in TURN-only mode, server should also use TURN-only
	clientUsesRelayOnly := !strings.Contains(offerSDP, "typ host") &&
		!strings.Contains(offerSDP, "typ srflx") &&
		strings.Contains(offerSDP, "typ relay")

	if clientUsesRelayOnly && len(turnCredentials) > 0 {
		pcConfig.ICETransportPolicy = webrtc.ICETransportPolicyRelay
		logger.Base().Info("Client using TURN-only mode, server also forcing relay mode")
	}

	pc, err := api.NewPeerConnection(pcConfig)
	if err != nil {
		return nil, err
	}

	// 4) Create SINGLE audio transceiver BEFORE setting remote description (critical!)
	// This ensures we only have ONE m=audio line to match WA's offer
	transceiver, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv})
	if err != nil {
		return nil, err
	}
	logger.Base().Info("Created single audio transceiver (sendrecv) to match WA's m=audio")

	// 5) Set up OnTrack handler for receiving WA audio (replaces InputTrack concept)
	pc.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		logger.Base().Info("WA inbound track", zap.String("codec", remote.Codec().MimeType), zap.Any("ssrc", remote.SSRC()))
		// Note: Audio forwarding to AI will be handled in webrtc_processor.go
	})

	// 6) Set remote description (WA's offer) BEFORE creating output track
	if err = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		return nil, err
	}

	// 7) Create output track for sending AI audio to WA
	outputTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: config.DefaultSampleRate,
			Channels:  config.DefaultChannelsMono, // Explicit MONO - must match MediaEngine
		}, "audio", "wa-output")
	if err != nil {
		return nil, err
	}
	logger.Base().Info("Created MONO output track (AI->WA)")

	// 8) CRITICAL: Use ReplaceTrack instead of AddTrack to bind to same transceiver
	// This ensures we don't create a second m=audio line
	if err := transceiver.Sender().ReplaceTrack(outputTrack); err != nil {
		return nil, err
	}
	logger.Base().Info("Bound output track to existing transceiver (no new m=audio)")

	// 9) Generate & set local SDP (NO manual editing)
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}

	if err = pc.SetLocalDescription(answer); err != nil {
		return nil, err
	}

	// 10) Wait for ICE gathering to complete (non-trickle)
	<-webrtc.GatheringCompletePromise(pc)

	// 11) Return Pion's SDP directly - NO manual editing to ensure SSRC consistency
	finalSDP := pc.LocalDescription().SDP

	// Log ICE candidate statistics for debugging
	candidateLines := 0
	relayCount := 0
	srflxCount := 0
	hostCount := 0
	for _, line := range strings.Split(finalSDP, "\n") {
		if strings.HasPrefix(line, "a=candidate:") {
			candidateLines++
			if strings.Contains(line, "typ relay") {
				relayCount++
			} else if strings.Contains(line, "typ srflx") {
				srflxCount++
			} else if strings.Contains(line, "typ host") {
				hostCount++
			}
		}
	}
	logger.Base().Info("Generated SDP with single m=audio and consistent SSRC")
	logger.Base().Info("Server ICE candidates", zap.Int("total", candidateLines), zap.Int("relay", relayCount), zap.Int("srflx", srflxCount), zap.Int("host", hostCount))

	// 12) Start RTCP monitoring to verify WA receives our audio (minimal logging)
	go func() {
		rtcpCount := 0
		for {
			pkts, _, err := transceiver.Sender().ReadRTCP()
			if err != nil {
				return
			}
			for _, p := range pkts {
				rtcpCount++
				// Only log every 500th RTCP packet to reduce noise
				if rtcpCount%500 == 0 {
					switch pkt := p.(type) {
					case *rtcp.ReceiverReport:
						if len(pkt.Reports) > 0 {
							logger.Base().Info("RTCP: Audio delivery confirmed", zap.Int("packet_count", rtcpCount))
						}
					}
				}
			}
		}
	}()

	return &ProcessOfferResult{
		AnswerSDP:    finalSDP,
		PC:           pc,
		OutputTrack:  outputTrack,
		OutputSender: transceiver.Sender(),
		Transceiver:  transceiver,
	}, nil
}

// PionOpusWriter encapsulates Pion's StaticSample track and sender for WriteSample usage
type PionOpusWriter struct {
	track      *webrtc.TrackLocalStaticSample
	sender     *webrtc.RTPSender
	frameCount int // Counter for reduced logging
}

// NewPionOpusWriter creates a new Pion Opus writer
func NewPionOpusWriter(track *webrtc.TrackLocalStaticSample, sender *webrtc.RTPSender) *PionOpusWriter {
	return &PionOpusWriter{
		track:  track,
		sender: sender,
	}
}

// StartRTCPMonitoring starts monitoring RTCP feedback to check if remote receives our audio
func (w *PionOpusWriter) StartRTCPMonitoring() {
	go func() {
		rtcpCount := 0
		for {
			pkts, _, err := w.sender.ReadRTCP()
			if err != nil {
				return
			}
			for _, p := range pkts {
				rtcpCount++
				// Only log every 200th RTCP packet to reduce spam
				if rtcpCount%200 == 0 {
					switch p.(type) {
					case *rtcp.ReceiverReport:
						logger.Base().Info("RTCP: Audio delivery active", zap.Int("report_count", rtcpCount))
					}
				}
			}
		}
	}()
}

// WriteOpusFrame writes an Opus frame using Pion's StaticSample API (20ms frame)
func (w *PionOpusWriter) WriteOpusFrame(opusData []byte) error {
	// Skip only completely empty frames
	if len(opusData) == 0 {
		return nil
	}

	sample := media.Sample{
		Data:     opusData,
		Duration: config.DefaultFrameDuration, // 20ms frame as recommended
	}

	if err := w.track.WriteSample(sample); err != nil {
		logger.Base().Error("Failed to write Opus sample")
		return err
	}

	// Minimal logging - only log every 500th frame to avoid spam
	if w.frameCount%500 == 0 {
		logger.Base().Info("Audio frames sent", zap.Int("frame_count", w.frameCount), zap.Int("bytes_per_frame", len(opusData)))
	}
	w.frameCount++

	return nil
}
