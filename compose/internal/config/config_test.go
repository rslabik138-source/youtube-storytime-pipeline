package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesSettingsYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	content := `
background_dir: ../background/output
voiceover_dir: ../voiceover/output
avatar_dir: ../avatar/output
output_dir: output
prefer_nvenc: true
nvenc_bitrate: 6M
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings.yaml: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.BackgroundDir != "../background/output" || s.AvatarDir != "../avatar/output" {
		t.Fatalf("unexpected settings: %+v", s)
	}
	if !s.PreferNVENC || s.NVENCBitrate != "6M" {
		t.Fatalf("unexpected encoder settings: %+v", s)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.PortraitFile != "portrait.png" {
		t.Fatalf("expected default portrait_file 'portrait.png' (matches avatar's real output), got %q", s.PortraitFile)
	}
	if s.OutputWidth != 1920 || s.OutputHeight != 1080 || s.OutputFPS != 30 {
		t.Fatalf("unexpected default output dims: %dx%d@%d", s.OutputWidth, s.OutputHeight, s.OutputFPS)
	}
	if s.FFmpegCmd != "ffmpeg" || s.FFprobeCmd != "ffprobe" || s.RembgCmd != "rembg" {
		t.Fatalf("unexpected default tool commands: %+v", s)
	}
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}
