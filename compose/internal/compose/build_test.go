package compose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/placeholder/compose/internal/config"
	"github.com/placeholder/compose/internal/ffmpeg"
	"github.com/placeholder/compose/internal/rembg"
)

const testTimingJSON = `{
	"id": "s1", "voice": "af_aoede", "total_seconds": 12.5,
	"chapters": [{"index": 1, "beat": "hook", "start": 0, "end": 12.5}],
	"chunks": [
		{"index": 0, "chapter": 1, "start": 0, "end": 12.5, "text": "Hello there. This is a test chunk with several words in it."}
	]
}`

// testEnv lays out a real temp directory tree matching the cross-module
// contract compose reads: <bg>/<id>/bg.mp4, <voice>/<id>/{voice.wav,
// timing.json}, <avatar>/<id>/portrait.png — real files on disk (fake
// content, since only their EXISTENCE and, for timing.json, real JSON
// content matters to compose's own code).
func testEnv(t *testing.T) (config.Settings, config.Layout) {
	t.Helper()
	root := t.TempDir()
	id := "s1"

	bgDir := filepath.Join(root, "background", id)
	voiceDir := filepath.Join(root, "voiceover", id)
	avatarDir := filepath.Join(root, "avatar", id)
	for _, d := range []string{bgDir, voiceDir, avatarDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	mustWrite := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	mustWrite(filepath.Join(bgDir, "bg.mp4"), "fake-bg")
	mustWrite(filepath.Join(voiceDir, "voice.wav"), "fake-voice")
	mustWrite(filepath.Join(voiceDir, "timing.json"), testTimingJSON)
	mustWrite(filepath.Join(avatarDir, "portrait.png"), "fake-portrait")

	musicPath := filepath.Join(root, "music.mp3")
	mustWrite(musicPath, "fake-music")
	logoPath := filepath.Join(root, "logo.png")
	mustWrite(logoPath, "fake-logo")

	cfg := config.Settings{
		BackgroundDir: filepath.Join(root, "background"), VoiceoverDir: filepath.Join(root, "voiceover"), AvatarDir: filepath.Join(root, "avatar"),
		BackgroundFile: "bg.mp4", VoiceFile: "voice.wav", TimingFile: "timing.json", PortraitFile: "portrait.png",
		OutputDir: filepath.Join(root, "output"), CutoutCacheDir: filepath.Join(root, "cache"),
		MusicFile: musicPath, MusicLicenseNote: "CC0 test fixture", LogoFile: logoPath,
		OutputWidth: 1920, OutputHeight: 1080, OutputFPS: 30,
		NVENCPreset: "p4", NVENCBitrate: "4M", LibX264Preset: "faster",
		PreferNVENC: true,
	}
	// Load a real defaulted layout (an empty file gets every applyDefaults
	// value) — the caption-card renderer needs real colors/sizes, not a
	// zero-value Layout.
	layoutPath := filepath.Join(root, "layout.yaml")
	if err := os.WriteFile(layoutPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write layout.yaml: %v", err)
	}
	layout, err := config.LoadLayout(layoutPath)
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	return cfg, layout
}

func TestBuildHappyPathUsesPreferredEncoder(t *testing.T) {
	cfg, layout := testEnv(t)
	rembgRunner := &rembg.FakeRunner{}
	ffmpegRunner := &ffmpeg.FakeRunner{}

	result, err := Build(context.Background(), cfg, layout, rembgRunner, ffmpegRunner, Options{ID: "s1"}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.EncoderUsed != "h264_nvenc" {
		t.Fatalf("expected nvenc to be used when PreferNVENC is true and it succeeds, got %q", result.EncoderUsed)
	}
	if result.DurationSeconds != 12.5 {
		t.Fatalf("expected the full timing.json duration (12.5), got %v", result.DurationSeconds)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutputDir, "s1", "captions", "captions.ffconcat")); err != nil {
		t.Fatalf("expected the caption track to be written: %v", err)
	}
	if len(rembgRunner.Calls) != 1 {
		t.Fatalf("expected rembg to run once, got %d calls", len(rembgRunner.Calls))
	}
	if len(ffmpegRunner.RunCalls) != 1 {
		t.Fatalf("expected exactly 1 ffmpeg run (nvenc succeeded, no fallback needed), got %d", len(ffmpegRunner.RunCalls))
	}
}

func TestBuildFallsBackToLibX264WhenNVENCFails(t *testing.T) {
	cfg, layout := testEnv(t)
	rembgRunner := &rembg.FakeRunner{}
	ffmpegRunner := &ffmpeg.FakeRunner{FailArgsContaining: "h264_nvenc"}

	result, err := Build(context.Background(), cfg, layout, rembgRunner, ffmpegRunner, Options{ID: "s1"}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.EncoderUsed != "libx264" {
		t.Fatalf("expected fallback to libx264, got %q", result.EncoderUsed)
	}
	if len(ffmpegRunner.RunCalls) != 2 {
		t.Fatalf("expected 2 ffmpeg runs (nvenc attempt + libx264 fallback), got %d", len(ffmpegRunner.RunCalls))
	}
}

func TestBuildPreviewCapsDuration(t *testing.T) {
	cfg, layout := testEnv(t)
	rembgRunner := &rembg.FakeRunner{}
	ffmpegRunner := &ffmpeg.FakeRunner{}

	result, err := Build(context.Background(), cfg, layout, rembgRunner, ffmpegRunner, Options{ID: "s1", PreviewSeconds: 3}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.DurationSeconds != 3 {
		t.Fatalf("expected preview duration to cap at 3s, got %v", result.DurationSeconds)
	}
}

func TestBuildPreviewLongerThanFullDurationUsesFullDuration(t *testing.T) {
	cfg, layout := testEnv(t)
	result, err := Build(context.Background(), cfg, layout, &rembg.FakeRunner{}, &ffmpeg.FakeRunner{}, Options{ID: "s1", PreviewSeconds: 999}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.DurationSeconds != 12.5 {
		t.Fatalf("expected the real duration when --preview exceeds it, got %v", result.DurationSeconds)
	}
}

func TestBuildNoSubsSkipsCaptionTrack(t *testing.T) {
	cfg, layout := testEnv(t)
	_, err := Build(context.Background(), cfg, layout, &rembg.FakeRunner{}, &ffmpeg.FakeRunner{}, Options{ID: "s1", NoSubs: true}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutputDir, "s1", "captions")); !os.IsNotExist(err) {
		t.Fatalf("expected no caption track to be written with --no-subs, stat err: %v", err)
	}
}

func TestBuildNoMusicDoesNotRequireMusicFile(t *testing.T) {
	cfg, layout := testEnv(t)
	cfg.MusicFile = filepath.Join(cfg.OutputDir, "does-not-exist.mp3") // would fail the Stat check if music were required
	_, err := Build(context.Background(), cfg, layout, &rembg.FakeRunner{}, &ffmpeg.FakeRunner{}, Options{ID: "s1", NoMusic: true}, nil)
	if err != nil {
		t.Fatalf("expected --no-music to skip the music-file check entirely, got: %v", err)
	}
}

func TestBuildMissingBackgroundReturnsClearError(t *testing.T) {
	cfg, layout := testEnv(t)
	if err := os.Remove(filepath.Join(cfg.BackgroundDir, "s1", "bg.mp4")); err != nil {
		t.Fatalf("remove bg.mp4: %v", err)
	}
	_, err := Build(context.Background(), cfg, layout, &rembg.FakeRunner{}, &ffmpeg.FakeRunner{}, Options{ID: "s1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "background module") {
		t.Fatalf("expected a clear missing-background error, got: %v", err)
	}
}

func TestBuildRefusesMusicWithoutALicenseNote(t *testing.T) {
	cfg, layout := testEnv(t)
	cfg.MusicLicenseNote = ""
	_, err := Build(context.Background(), cfg, layout, &rembg.FakeRunner{}, &ffmpeg.FakeRunner{}, Options{ID: "s1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "music_license_note") {
		t.Fatalf("expected a clear error about the blank music_license_note, got: %v", err)
	}
}

func TestBuildMissingIDReturnsError(t *testing.T) {
	cfg, layout := testEnv(t)
	if _, err := Build(context.Background(), cfg, layout, &rembg.FakeRunner{}, &ffmpeg.FakeRunner{}, Options{}, nil); err == nil {
		t.Fatalf("expected an error for an empty id")
	}
}

func TestBuildReusesCachedCutoutOnSecondCall(t *testing.T) {
	cfg, layout := testEnv(t)
	rembgRunner := &rembg.FakeRunner{}
	if _, err := Build(context.Background(), cfg, layout, rembgRunner, &ffmpeg.FakeRunner{}, Options{ID: "s1"}, nil); err != nil {
		t.Fatalf("first Build: %v", err)
	}
	if _, err := Build(context.Background(), cfg, layout, rembgRunner, &ffmpeg.FakeRunner{}, Options{ID: "s1"}, nil); err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if len(rembgRunner.Calls) != 1 {
		t.Fatalf("expected rembg to run only once across 2 builds of the same id, got %d", len(rembgRunner.Calls))
	}
}
