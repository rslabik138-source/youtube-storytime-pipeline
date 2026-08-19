// Package compose orchestrates the final render: gather inputs from the
// other four modules' outputs, prep the portrait (rembg), build subtitles
// from voiceover's timing, then hand everything to ffmpeg.
package compose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/placeholder/compose/internal/config"
	"github.com/placeholder/compose/internal/ffmpeg"
	"github.com/placeholder/compose/internal/manifest"
	"github.com/placeholder/compose/internal/rembg"
	"github.com/placeholder/compose/internal/subtitles"
)

// Options controls one `compose build` run.
type Options struct {
	ID             string
	NoMusic        bool
	NoSubs         bool
	PreviewSeconds float64 // 0 disables preview (full render)
}

// Result is what a successful Build produced.
type Result struct {
	OutputPath      string
	EncoderUsed     string
	DurationSeconds float64
}

// Logf receives progress lines — e.g. "prep: reusing cached cutout",
// "encoder: h264_nvenc failed, falling back to libx264". nil is fine.
type Logf func(format string, args ...any)

// Build runs the whole pipeline for one script id. It never partially
// writes output.mp4 — ffmpeg's own -y overwrite plus writing to the real
// output path only on success (RunWithFallback returns an error, not a
// half-written file, on total failure) means a failed build leaves
// whatever (if anything) was there before untouched.
func Build(ctx context.Context, cfg config.Settings, layout config.Layout, rembgRunner rembg.Runner, ffmpegRunner ffmpeg.Runner, opts Options, log Logf) (Result, error) {
	if log == nil {
		log = func(string, ...any) {}
	}
	if opts.ID == "" {
		return Result{}, fmt.Errorf("compose: id is required")
	}

	in, err := resolveInputs(cfg, opts.ID)
	if err != nil {
		return Result{}, err
	}

	timing, err := manifest.LoadTiming(in.timingPath)
	if err != nil {
		return Result{}, fmt.Errorf("compose: load %s (did you run `voice generate %s` first?): %w", in.timingPath, opts.ID, err)
	}

	outDir := filepath.Join(cfg.OutputDir, opts.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("compose: create %s: %w", outDir, err)
	}

	log("prep: preparing portrait cutout for %s", opts.ID)
	cutoutPath, err := rembg.EnsureCutout(ctx, rembgRunner, in.portraitPath, cfg.CutoutCacheDir, opts.ID)
	if err != nil {
		return Result{}, fmt.Errorf("compose: portrait cutout: %w", err)
	}

	duration := timing.TotalSeconds
	if opts.PreviewSeconds > 0 && opts.PreviewSeconds < duration {
		duration = opts.PreviewSeconds
		log("preview: capping render to %.0fs of %.0fs", duration, timing.TotalSeconds)
	}

	captionTrackPath := ""
	if !opts.NoSubs {
		log("prep: building caption cards from %d chunks", len(timing.Chunks))
		words := subtitles.InterpolateAll(timing.Chunks)
		lines := subtitles.GroupLines(words, layout.Subtitles.WordsPerLineMin, layout.Subtitles.WordsPerLineMax)
		style, err := cardStyleFromLayout(layout)
		if err != nil {
			return Result{}, err
		}
		track, err := subtitles.WriteTrack(lines, style, cfg.OutputWidth, cfg.OutputHeight, duration, filepath.Join(outDir, "captions"))
		if err != nil {
			return Result{}, fmt.Errorf("compose: build caption track: %w", err)
		}
		log("prep: %d caption cards rendered", track.CardCount)
		captionTrackPath = track.ConcatPath
	}

	// Music is off unless a music_file is configured (and --no-music isn't
	// set) — a blank music_file means "this channel runs without background
	// music", same opt-in-by-config approach as the logo.
	musicPath := ""
	includeMusic := !opts.NoMusic && strings.TrimSpace(cfg.MusicFile) != ""
	if includeMusic {
		if _, err := os.Stat(cfg.MusicFile); err != nil {
			return Result{}, fmt.Errorf("compose: music file %s (see settings.yaml's music_file, or pass --no-music): %w", cfg.MusicFile, err)
		}
		if strings.TrimSpace(cfg.MusicLicenseNote) == "" {
			return Result{}, fmt.Errorf("compose: settings.yaml's music_license_note is blank — document where %s's royalty-free license lives before shipping it, or pass --no-music", cfg.MusicFile)
		}
		musicPath = cfg.MusicFile
	}

	// No logo_file configured means the channel runs without a logo — skip
	// the layer entirely rather than overlaying a missing/placeholder file.
	includeLogo := strings.TrimSpace(cfg.LogoFile) != ""
	if includeLogo {
		if _, err := os.Stat(cfg.LogoFile); err != nil {
			return Result{}, fmt.Errorf("compose: logo file %s (see settings.yaml's logo_file, or blank it out to render without a logo): %w", cfg.LogoFile, err)
		}
	}

	ffInputs := ffmpeg.Inputs{
		Background: in.backgroundPath, Voice: in.voicePath, PortraitCutout: cutoutPath,
		Music: musicPath, Logo: cfg.LogoFile,
	}
	buildOpts := ffmpeg.BuildOptions{
		Width: cfg.OutputWidth, Height: cfg.OutputHeight, FPS: cfg.OutputFPS, Layout: layout,
		IncludeMusic: includeMusic, IncludeSubs: !opts.NoSubs, IncludeLogo: includeLogo,
		CaptionTrackPath: captionTrackPath,
		DurationSeconds:  duration,
	}

	outputPath := filepath.Join(outDir, "final.mp4")
	buildArgs := func(enc ffmpeg.Encoder) []string { return ffmpeg.BuildArgs(ffInputs, buildOpts, enc, outputPath) }

	primary := ffmpeg.LibX264Encoder(cfg)
	if cfg.PreferNVENC {
		primary = ffmpeg.NVENCEncoder(cfg)
	}
	fallback := ffmpeg.LibX264Encoder(cfg)

	log("render: encoding %.0fs of video", duration)
	used, err := ffmpeg.RunWithFallback(ctx, ffmpegRunner, buildArgs, primary, fallback, func(primaryErr error) {
		log("encoder: %s failed (%v), falling back to %s", primary.Name, primaryErr, fallback.Name)
	})
	if err != nil {
		return Result{}, fmt.Errorf("compose: render: %w", err)
	}

	return Result{OutputPath: outputPath, EncoderUsed: used.Name, DurationSeconds: duration}, nil
}

// cardStyleFromLayout resolves the caption-card style from config —
// parsing the hex colors and loading the font file (empty falls back to
// the embedded bold font, so a render never fails for a missing font).
func cardStyleFromLayout(layout config.Layout) (subtitles.CardStyle, error) {
	s := layout.Subtitles

	textColor, err := config.ParseHexColor(s.TextColor, 1.0)
	if err != nil {
		return subtitles.CardStyle{}, fmt.Errorf("compose: subtitle text_color: %w", err)
	}
	boxColor, err := config.ParseHexColor(s.BoxColor, s.BoxOpacity)
	if err != nil {
		return subtitles.CardStyle{}, fmt.Errorf("compose: subtitle box_color: %w", err)
	}

	var fontBytes []byte
	if s.FontFile != "" {
		fontBytes, err = os.ReadFile(s.FontFile)
		if err != nil {
			return subtitles.CardStyle{}, fmt.Errorf("compose: subtitle font_file %s (leave blank for the embedded bold font): %w", s.FontFile, err)
		}
	}

	return subtitles.CardStyle{
		FontBytes: fontBytes, FontSize: s.FontSize,
		TextColor: textColor, BoxColor: boxColor,
		CornerRadius: s.CornerRadius, PaddingX: s.PaddingX, PaddingY: s.PaddingY,
		LineSpacing: s.LineSpacing, MaxTextWidth: s.MaxWidth,
		ZoneLeft: s.ZoneLeft, ZoneRight: s.ZoneRight,
	}, nil
}

type inputs struct {
	backgroundPath string
	voicePath      string
	timingPath     string
	portraitPath   string
}

// resolveInputs builds every input path and checks each file actually
// exists up front — a missing input fails fast with a clear "run this
// other module first" message instead of a confusing ffmpeg error deep
// into the render.
func resolveInputs(cfg config.Settings, id string) (inputs, error) {
	in := inputs{
		backgroundPath: filepath.Join(cfg.BackgroundDir, id, cfg.BackgroundFile),
		voicePath:      filepath.Join(cfg.VoiceoverDir, id, cfg.VoiceFile),
		timingPath:     filepath.Join(cfg.VoiceoverDir, id, cfg.TimingFile),
		portraitPath:   filepath.Join(cfg.AvatarDir, id, cfg.PortraitFile),
	}
	checks := []struct{ path, hint string }{
		{in.backgroundPath, "the background module"},
		{in.voicePath, "`voice generate " + id + "`"},
		{in.portraitPath, "`avatar generate " + id + "`"},
	}
	for _, c := range checks {
		if _, err := os.Stat(c.path); err != nil {
			return inputs{}, fmt.Errorf("compose: missing input %s (produced by %s): %w", c.path, c.hint, err)
		}
	}
	return in, nil
}
