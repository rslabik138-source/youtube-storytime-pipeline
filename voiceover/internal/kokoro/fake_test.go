package kokoro

import (
	"context"
	"testing"
	"time"
)

func TestFakeSynthSpeakReturnsValidWAVHeader(t *testing.T) {
	f := NewFakeSynth(60)
	wav, err := f.Speak(context.Background(), "hello world", "af_bella", 1.0)
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if len(wav) < 44 {
		t.Fatalf("expected at least a 44-byte WAV header, got %d bytes", len(wav))
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("expected a valid RIFF/WAVE header, got: %q", wav[0:12])
	}
}

func TestFakeSynthDurationScalesWithTextLength(t *testing.T) {
	f := NewFakeSynth(60)
	short, err := f.Speak(context.Background(), "hi", "af_bella", 1.0)
	if err != nil {
		t.Fatalf("Speak short: %v", err)
	}
	long, err := f.Speak(context.Background(), "this is a much longer sentence than the short one", "af_bella", 1.0)
	if err != nil {
		t.Fatalf("Speak long: %v", err)
	}
	if len(long) <= len(short) {
		t.Fatalf("expected longer text to produce more audio bytes: short=%d long=%d", len(short), len(long))
	}
}

func TestFakeSynthSpeedAffectsDuration(t *testing.T) {
	f := NewFakeSynth(60)
	normal, err := f.Speak(context.Background(), "a reasonably long test sentence here", "af_bella", 1.0)
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	fast, err := f.Speak(context.Background(), "a reasonably long test sentence here", "af_bella", 2.0)
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if len(fast) >= len(normal) {
		t.Fatalf("expected 2x speed to produce less audio: normal=%d fast=%d", len(normal), len(fast))
	}
}

func TestFakeSynthFailOnForcesError(t *testing.T) {
	f := &FakeSynth{MsPerChar: 60, FailOn: "BOOM"}
	if _, err := f.Speak(context.Background(), "this text contains BOOM in it", "af_bella", 1.0); err == nil {
		t.Fatalf("expected an error when text contains FailOn substring")
	}
	if _, err := f.Speak(context.Background(), "this text is fine", "af_bella", 1.0); err != nil {
		t.Fatalf("expected no error for text without the FailOn substring, got: %v", err)
	}
}

func TestFakeSynthRespectsContextCancellation(t *testing.T) {
	f := NewFakeSynth(60)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Speak(ctx, "hello", "af_bella", 1.0); err == nil {
		t.Fatalf("expected an error for an already-cancelled context")
	}
}

func TestFakeSynthVoicesDefaultsAndOverride(t *testing.T) {
	f := NewFakeSynth(60)
	voices, err := f.Voices(context.Background())
	if err != nil || len(voices) == 0 {
		t.Fatalf("expected a non-empty default voice list, got %v err=%v", voices, err)
	}

	f.VoiceList = []string{"custom_voice"}
	voices, err = f.Voices(context.Background())
	if err != nil || len(voices) != 1 || voices[0] != "custom_voice" {
		t.Fatalf("expected the overridden voice list, got %v err=%v", voices, err)
	}
}

func TestSilenceWAVDataSizeMatchesDuration(t *testing.T) {
	wav := silenceWAV(1 * time.Second)
	// 24000 Hz, 16-bit, mono: 48000 bytes of data for 1 second, +44 byte header.
	wantLen := 44 + 24000*2
	if len(wav) != wantLen {
		t.Fatalf("expected %d bytes for 1s of 24kHz/16-bit/mono silence, got %d", wantLen, len(wav))
	}
}
