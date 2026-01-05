package storage

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

// OggOpusEncoder handles encoding PCM samples into Ogg Opus format using Pion
type OggOpusEncoder struct {
}

// NewOggOpusEncoder creates a new Ogg Opus encoder
func NewOggOpusEncoder() *OggOpusEncoder {
	return &OggOpusEncoder{}
}

// CreateOggOpusFile creates a complete Ogg Opus file from RTP packets using Pion OGG writer
func (e *OggOpusEncoder) CreateOggOpusFile(rtpPackets []*rtp.Packet) ([]byte, error) {
	return e.CreateOggOpusFileWithChannels(rtpPackets, 2) // Default to stereo for multi-channel support
}

// CreateOggOpusFileWithChannels creates a complete Ogg Opus file with specified number of channels
func (e *OggOpusEncoder) CreateOggOpusFileWithChannels(rtpPackets []*rtp.Packet, channels int) ([]byte, error) {
	if len(rtpPackets) == 0 {
		return nil, fmt.Errorf("no RTP packets to process")
	}

	// Validate channel count
	if channels < 1 || channels > 2 {
		return nil, fmt.Errorf("unsupported channel count: %d (must be 1 or 2)", channels)
	}

	// Create a buffer to write the OGG file
	var buffer bytes.Buffer

	// Create OGG writer for Opus with specified channels
	oggWriter, err := oggwriter.NewWith(&buffer, 48000, uint16(channels)) // 48kHz, configurable channels
	if err != nil {
		return nil, fmt.Errorf("failed to create OGG writer: %v", err)
	}
	defer oggWriter.Close()

	// Timestamps are already converted to WhatsApp standard in cache
	// Just write the packets directly to OGG writer
	for _, rtpPacket := range rtpPackets {
		if rtpPacket == nil || len(rtpPacket.Payload) == 0 {
			continue
		}

		// Write RTP packet directly (timestamps already normalized)
		if err := oggWriter.WriteRTP(rtpPacket); err != nil {
			return nil, fmt.Errorf("failed to write RTP packet to OGG: %v", err)
		}
	}

	return buffer.Bytes(), nil
}

// CreateStereoOggOpusFile creates a stereo Ogg Opus file with time-aligned left and right channel data
func (e *OggOpusEncoder) CreateStereoOggOpusFile(leftChannel, rightChannel []*rtp.Packet) ([]byte, error) {
	// Create a buffer to write the OGG file
	var buffer bytes.Buffer

	// Create OGG writer for stereo Opus (2 channels)
	oggWriter, err := oggwriter.NewWith(&buffer, 48000, 2) // 48kHz, stereo
	if err != nil {
		return nil, fmt.Errorf("failed to create OGG writer: %v", err)
	}
	defer oggWriter.Close()

	// 合并两个声道，按时间戳排序，创建时间对齐的立体声
	allPackets := make([]*rtp.Packet, 0, len(leftChannel)+len(rightChannel))

	// 添加左声道包（WhatsApp输入）
	for _, pkt := range leftChannel {
		if pkt != nil && len(pkt.Payload) > 0 {
			allPackets = append(allPackets, pkt)
		}
	}

	for _, pkt := range rightChannel {
		if pkt != nil && len(pkt.Payload) > 0 {
			allPackets = append(allPackets, pkt)
		}
	}

	// 按时间戳排序，确保正确的时序
	sort.Slice(allPackets, func(i, j int) bool {
		return allPackets[i].Timestamp < allPackets[j].Timestamp
	})

	// 写入排序后的包
	for _, rtpPacket := range allPackets {
		if err := oggWriter.WriteRTP(rtpPacket); err != nil {
			return nil, fmt.Errorf("failed to write RTP packet: %v", err)
		}
	}

	return buffer.Bytes(), nil
}

// CreateMonoOggOpusFile creates a mono Ogg Opus file from RTP packets with silence padding
func (e *OggOpusEncoder) CreateMonoOggOpusFile(rtpPackets []*rtp.Packet, totalDuration time.Duration) ([]byte, error) {
	if len(rtpPackets) == 0 {
		return nil, fmt.Errorf("no RTP packets to process")
	}

	// Create a buffer to write the OGG file
	var buffer bytes.Buffer

	// Create OGG writer for mono Opus (1 channel)
	oggWriter, err := oggwriter.NewWith(&buffer, 48000, 1) // 48kHz, mono
	if err != nil {
		return nil, fmt.Errorf("failed to create OGG writer: %v", err)
	}
	defer oggWriter.Close()

	// Sort packets by timestamp to ensure correct order
	sort.Slice(rtpPackets, func(i, j int) bool {
		return rtpPackets[i].Timestamp < rtpPackets[j].Timestamp
	})

	// Calculate total samples needed for the duration
	totalSamples := uint32(totalDuration.Milliseconds() * 48) // 48kHz sample rate
	const frameSamples = 960                                  // 20ms frame at 48kHz

	// Create timeline with silence padding
	currentTimestamp := uint32(0)
	packetIndex := 0
	for currentTimestamp < totalSamples {
		// Check if we have a packet for this timestamp
		if packetIndex < len(rtpPackets) && rtpPackets[packetIndex].Timestamp <= currentTimestamp {
			// Write the actual audio packet
			if err := oggWriter.WriteRTP(rtpPackets[packetIndex]); err != nil {
				return nil, fmt.Errorf("failed to write RTP packet: %v", err)
			}
			packetIndex++
		} else {
			// Create silence packet for this timestamp
			silencePacket := &rtp.Packet{
				Header: rtp.Header{
					Version:     2,
					PayloadType: 111, // Opus payload type
					SSRC:        1,
					Timestamp:   currentTimestamp,
				},
				Payload: e.createSilenceOpusFrame(), // Create silence Opus frame
			}
			if err := oggWriter.WriteRTP(silencePacket); err != nil {
				return nil, fmt.Errorf("failed to write silence packet: %v", err)
			}
		}

		currentTimestamp += frameSamples
	}

	return buffer.Bytes(), nil
}

// createSilenceOpusFrame creates a silence Opus frame
func (e *OggOpusEncoder) createSilenceOpusFrame() []byte {
	// Create a proper Opus DTX (Discontinuous Transmission) silence frame
	// TOC byte: 0xF8 (frame count = 1, stereo = 0, config = 8) for DTX
	// DTX frames should be minimal to avoid artifacts
	return []byte{0xF8} // Proper DTX frame
}
