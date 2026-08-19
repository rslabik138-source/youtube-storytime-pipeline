package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLayoutParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "layout.yaml")
	content := `
background:
  blur: "10:1"
  brightness: -0.25
subtitles:
  font_size: 60
  words_per_line_min: 3
  words_per_line_max: 6
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write layout.yaml: %v", err)
	}
	l, err := LoadLayout(path)
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	if l.Background.Blur != "10:1" || l.Background.Brightness != -0.25 {
		t.Fatalf("unexpected background layout: %+v", l.Background)
	}
	if l.Subtitles.FontSize != 60 || l.Subtitles.WordsPerLineMin != 3 || l.Subtitles.WordsPerLineMax != 6 {
		t.Fatalf("unexpected subtitle layout: %+v", l.Subtitles)
	}
	// Untouched sections still get their defaults.
	if l.Waveform.Width != 1536 {
		t.Fatalf("expected default waveform width, got %d", l.Waveform.Width)
	}
}

func TestLoadLayoutAppliesDefaultsMatchingTheSpec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layout.yaml")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	l, err := LoadLayout(path)
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	if l.Portrait.Height != 864 || l.Portrait.X != 40 {
		t.Fatalf("unexpected portrait defaults: %+v", l.Portrait)
	}
	if l.Waveform.Width != 1536 || l.Waveform.Height != 160 || l.Waveform.BottomMargin != 20 {
		t.Fatalf("unexpected waveform defaults: %+v", l.Waveform)
	}
	if l.Waveform.Draw != "full" {
		t.Fatalf("expected draw=full (the fix for the invisible waveform), got %q", l.Waveform.Draw)
	}
	if l.Waveform.Gain != 1 || l.Waveform.Scale != "lin" {
		t.Fatalf("expected unity gain / linear scale with draw=full, got gain=%v scale=%q", l.Waveform.Gain, l.Waveform.Scale)
	}
	if l.Subtitles.FontSize != 54 || l.Subtitles.TextColor != "#FFFFFF" || l.Subtitles.BoxColor != "#000000" {
		t.Fatalf("unexpected subtitle defaults: %+v", l.Subtitles)
	}
	if l.Subtitles.BoxOpacity != 0.85 || l.Subtitles.CornerRadius != 20 || l.Subtitles.LineSpacing != 1.15 {
		t.Fatalf("unexpected card-style defaults: %+v", l.Subtitles)
	}
	if l.Subtitles.ZoneLeft != 950 || l.Subtitles.ZoneRight != 60 {
		t.Fatalf("expected default zone that clears a bottom-left portrait, got L=%d R=%d", l.Subtitles.ZoneLeft, l.Subtitles.ZoneRight)
	}
	if l.Music.Volume != 0.12 || l.Music.DuckThreshold != 0.03 || l.Music.DuckRatio != 8 {
		t.Fatalf("unexpected music defaults: %+v", l.Music)
	}
}

// TestWaveformYIsDerivedFromHeightAndMargin locks in that the waveform's
// Y is always H - Height - BottomMargin (never a stored magic number), so
// changing Height alone can't silently break how much bottom margin is
// left — with the current defaults (160 + 60) that's H-220.
func TestWaveformYIsDerivedFromHeightAndMargin(t *testing.T) {
	l := Layout{}
	l.applyDefaults()
	got := l.WaveformY(1080)
	if got != 1080-l.Waveform.Height-l.Waveform.BottomMargin {
		t.Fatalf("expected WaveformY == H - Height - BottomMargin, got %d", got)
	}
}
