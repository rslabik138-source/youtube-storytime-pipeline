package kokoro

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// FakeSynth is a Synthesizer that never touches the network — it returns a
// real, valid WAV file of silence whose duration is proportional to the
// input text, so callers that measure actual audio duration (assemble's
// ffprobe-based timing) get plausible, deterministic numbers without
// Docker or Kokoro running.
type FakeSynth struct {
	// MsPerChar sets how many milliseconds of silence one input character
	// is worth — a crude stand-in for real speech pace, tunable per test.
	MsPerChar float64
	// VoiceList is what Voices() returns; nil falls back to a small
	// built-in default set.
	VoiceList []string
	// FailOn, if non-empty, makes Speak return an error whenever text
	// contains this substring — for testing a caller's error handling
	// without a real backend to misbehave on command.
	FailOn string
}

// NewFakeSynth builds a FakeSynth. msPerChar <= 0 defaults to 60ms/char, a
// roughly conversational pace for synthetic test audio.
func NewFakeSynth(msPerChar float64) *FakeSynth {
	if msPerChar <= 0 {
		msPerChar = 60
	}
	return &FakeSynth{MsPerChar: msPerChar}
}

func (f *FakeSynth) Speak(ctx context.Context, text, voice string, speed float64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.FailOn != "" && strings.Contains(text, f.FailOn) {
		return nil, fmt.Errorf("fake kokoro: forced failure for text containing %q", f.FailOn)
	}
	if speed <= 0 {
		speed = 1.0
	}
	ms := float64(len(text)) * f.MsPerChar / speed
	return silenceWAV(time.Duration(ms) * time.Millisecond), nil
}

func (f *FakeSynth) Voices(ctx context.Context) ([]string, error) {
	if f.VoiceList != nil {
		return f.VoiceList, nil
	}
	return []string{"af_bella", "am_adam"}, nil
}

// silenceWAV builds a real, minimal RIFF/WAVE file (16-bit PCM mono, 24kHz
// — Kokoro's real default) containing d of digital silence. Real ffmpeg/
// ffprobe (assemble's package, not this one) can decode, concatenate, and
// measure this exactly like genuine synthesized audio.
func silenceWAV(d time.Duration) []byte {
	const sampleRate = 24000
	const bitsPerSample = 16
	const numChannels = 1

	numSamples := int(d.Seconds() * sampleRate)
	if numSamples < 1 {
		numSamples = 1
	}
	dataSize := numSamples * numChannels * (bitsPerSample / 8)
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8

	buf := new(bytes.Buffer)
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16)) // PCM fmt chunk size
	binary.Write(buf, binary.LittleEndian, uint16(1))  // 1 = PCM
	binary.Write(buf, binary.LittleEndian, uint16(numChannels))
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(buf, binary.LittleEndian, uint32(byteRate))
	binary.Write(buf, binary.LittleEndian, uint16(blockAlign))
	binary.Write(buf, binary.LittleEndian, uint16(bitsPerSample))
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	buf.Write(make([]byte, dataSize)) // all-zero bytes = silence
	return buf.Bytes()
}
