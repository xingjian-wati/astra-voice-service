package provider

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"time"

	"github.com/hajimehoshi/go-mp3"
	"layeh.com/gopus"
)

const (
	// DefaultBGMSilenceThreshold is the idle duration before inserting BGM frames.
	DefaultBGMSilenceThreshold = time.Second * 1
	// DefaultBGMPath is the default audio cue file; providers may override.
	DefaultBGMPath = "static/voice_agent_audio_cue.mp3"
)

type bgmCacheEntry struct {
	once   sync.Once
	frames [][]byte
	err    error
}

var bgmCache sync.Map // map[string]*bgmCacheEntry

// LoadBGMFrames loads and caches BGM Opus frames from an MP3 path.
// Frames are 20ms @ 48kHz mono Opus.
func LoadBGMFrames(path string) ([][]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("BGM path not configured")
	}

	entryAny, _ := bgmCache.LoadOrStore(path, &bgmCacheEntry{})
	entry := entryAny.(*bgmCacheEntry)

	entry.once.Do(func() {
		frames, err := encodeMP3ToOpusFrames(path)
		entry.frames = frames
		entry.err = err
	})

	return entry.frames, entry.err
}

func encodeMP3ToOpusFrames(path string) ([][]byte, error) {
	pcm, err := loadMP3PCM(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load BGM: %w", err)
	}

	encoder, err := gopus.NewEncoder(48000, 1, gopus.Audio)
	if err != nil {
		return nil, fmt.Errorf("failed to init opus encoder: %w", err)
	}

	const frameSize = 960 // 20ms @ 48kHz mono
	frames := make([][]byte, 0, len(pcm)/frameSize+1)

	for offset := 0; offset < len(pcm); offset += frameSize {
		samples := make([]int16, frameSize)
		end := offset + frameSize
		if end > len(pcm) {
			end = len(pcm)
		}
		copy(samples, pcm[offset:end])

		frame, err := encoder.Encode(samples, frameSize, frameSize*2)
		if err != nil {
			return nil, fmt.Errorf("failed to encode opus frame: %w", err)
		}
		frames = append(frames, frame)
	}

	if len(frames) == 0 {
		return nil, fmt.Errorf("no opus frames generated from BGM")
	}

	return frames, nil
}

// loadMP3PCM loads an MP3 file and returns mono PCM16 at 48kHz.
func loadMP3PCM(path string) ([]int16, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open BGM: %w", err)
	}
	defer file.Close()

	decoder, err := mp3.NewDecoder(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decode MP3: %w", err)
	}

	raw, err := io.ReadAll(decoder)
	if err != nil {
		return nil, fmt.Errorf("failed to read MP3 data: %w", err)
	}
	if len(raw) < 2 {
		return nil, fmt.Errorf("empty MP3 data")
	}

	if len(raw)%2 != 0 {
		raw = raw[:len(raw)-1]
	}

	var pcm []int16
	if len(raw)%4 == 0 {
		frames := len(raw) / 4 // 2 bytes * 2 channels
		pcm = make([]int16, frames)
		for i := 0; i < frames; i++ {
			l := int16(binary.LittleEndian.Uint16(raw[4*i:]))
			r := int16(binary.LittleEndian.Uint16(raw[4*i+2:]))
			pcm[i] = int16((int32(l) + int32(r)) / 2)
		}
	} else {
		pcm = make([]int16, len(raw)/2)
		for i := 0; i < len(pcm); i++ {
			pcm[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
		}
	}

	sampleRate := decoder.SampleRate()
	if sampleRate <= 0 {
		return nil, fmt.Errorf("invalid sample rate %d", sampleRate)
	}
	if sampleRate != 48000 {
		pcm = resampleLinearInt16(pcm, sampleRate, 48000)
	}

	return pcm, nil
}

// resampleLinearInt16 performs a simple linear resample to target sample rate.
func resampleLinearInt16(in []int16, fromRate, toRate int) []int16 {
	if len(in) == 0 || fromRate == toRate {
		return append([]int16(nil), in...)
	}

	ratio := float64(fromRate) / float64(toRate)
	outLen := int(math.Round(float64(len(in)) / ratio))
	if outLen <= 0 {
		return []int16{}
	}

	out := make([]int16, outLen)
	for i := 0; i < outLen; i++ {
		srcPos := float64(i) * ratio
		s0 := int(srcPos)
		if s0 >= len(in) {
			s0 = len(in) - 1
		}
		s1 := s0 + 1
		if s1 >= len(in) {
			s1 = len(in) - 1
		}
		frac := srcPos - float64(s0)
		out[i] = int16((1-frac)*float64(in[s0]) + frac*float64(in[s1]))
	}
	return out
}
