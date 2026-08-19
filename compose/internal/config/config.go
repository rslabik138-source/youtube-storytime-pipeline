// Package config loads settings.yaml (input/output paths, encoder choice,
// external tool commands) and layout.yaml (every position, size, color,
// and opacity the composition uses — see Layout).
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Settings is settings.yaml's shape.
type Settings struct {
	// Input directories — each holds <id>/<file> the same way scenario's
	// bundle export does. Defaults assume compose sits as a sibling
	// module, matching go.work's layout.
	BackgroundDir string `yaml:"background_dir"`
	VoiceoverDir  string `yaml:"voiceover_dir"`
	AvatarDir     string `yaml:"avatar_dir"`

	// Input file names within each <id>/ directory.
	BackgroundFile string `yaml:"background_file"`
	VoiceFile      string `yaml:"voice_file"`
	TimingFile     string `yaml:"timing_file"`
	// PortraitFile defaults to "portrait.png" — the file avatar's CLI
	// actually writes today, NOT "portrait-video.png". If a future avatar
	// change renames its output, this is the one line to update.
	PortraitFile string `yaml:"portrait_file"`

	OutputDir string `yaml:"output_dir"`

	// LogoFile is the top-left logo overlay. Blank (the default) renders
	// without any logo — compose skips the layer entirely.
	LogoFile  string `yaml:"logo_file"`
	MusicFile string `yaml:"music_file"`
	// MusicLicenseNote documents where MusicFile's royalty-free license
	// lives (a URL, a receipt path, "CC0") — required to be non-empty so
	// a real render can't ship a track nobody checked the rights on.
	MusicLicenseNote string `yaml:"music_license_note"`

	// RembgCmd is the rembg CLI's command name or full path — must be on
	// PATH or absolute. Requires `pip install rembg` (Python), a runtime
	// dependency this Go module cannot install for you.
	RembgCmd string `yaml:"rembg_cmd"`
	// CutoutCacheDir holds rembg's output (portrait-cutout.png) keyed by
	// id, so a second `compose build` on the same id never re-runs rembg.
	CutoutCacheDir string `yaml:"cutout_cache_dir"`

	OutputWidth  int `yaml:"output_width"`
	OutputHeight int `yaml:"output_height"`
	OutputFPS    int `yaml:"output_fps"`

	// PreferNVENC tries h264_nvenc first (hardware-encoded, several times
	// faster on a real GPU) and falls back to libx264 automatically if
	// ffmpeg reports nvenc unavailable (no GPU, driver too old, no ffmpeg
	// build with nvenc compiled in). YAML false and "unset" are the same
	// zero value, so this has no applyDefaults() fallback — settings.yaml
	// ships prefer_nvenc: true explicitly instead.
	PreferNVENC   bool   `yaml:"prefer_nvenc"`
	NVENCPreset   string `yaml:"nvenc_preset"`
	NVENCBitrate  string `yaml:"nvenc_bitrate"`
	LibX264Preset string `yaml:"libx264_preset"`
	FFmpegCmd     string `yaml:"ffmpeg_cmd"`
	FFprobeCmd    string `yaml:"ffprobe_cmd"`
}

func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	var s Settings
	if err := yaml.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	s.applyDefaults()
	return s, nil
}

func (s *Settings) applyDefaults() {
	if s.BackgroundDir == "" {
		s.BackgroundDir = "../background/output"
	}
	if s.VoiceoverDir == "" {
		s.VoiceoverDir = "../voiceover/output"
	}
	if s.AvatarDir == "" {
		s.AvatarDir = "../avatar/output"
	}
	if s.BackgroundFile == "" {
		s.BackgroundFile = "bg.mp4"
	}
	if s.VoiceFile == "" {
		s.VoiceFile = "voice.wav"
	}
	if s.TimingFile == "" {
		s.TimingFile = "timing.json"
	}
	if s.PortraitFile == "" {
		s.PortraitFile = "portrait.png"
	}
	if s.OutputDir == "" {
		s.OutputDir = "output"
	}
	// LogoFile and MusicFile intentionally have NO defaults — blank means
	// "render without a logo / without background music" (the channel's
	// choice), not "fall back to a placeholder asset".
	if s.RembgCmd == "" {
		s.RembgCmd = "rembg"
	}
	if s.CutoutCacheDir == "" {
		s.CutoutCacheDir = "cache/cutouts"
	}
	if s.OutputWidth == 0 {
		s.OutputWidth = 1920
	}
	if s.OutputHeight == 0 {
		s.OutputHeight = 1080
	}
	if s.OutputFPS == 0 {
		s.OutputFPS = 30
	}
	if s.NVENCPreset == "" {
		s.NVENCPreset = "p4"
	}
	if s.NVENCBitrate == "" {
		s.NVENCBitrate = "4M"
	}
	if s.LibX264Preset == "" {
		s.LibX264Preset = "faster"
	}
	if s.FFmpegCmd == "" {
		s.FFmpegCmd = "ffmpeg"
	}
	if s.FFprobeCmd == "" {
		s.FFprobeCmd = "ffprobe"
	}
}
