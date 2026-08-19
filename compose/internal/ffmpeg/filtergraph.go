// Package ffmpeg builds and runs the final composite's ffmpeg command:
// the -filter_complex graph (pure string building, fully unit-testable
// with no ffmpeg process involved) and the process execution/encoder
// fallback around it.
package ffmpeg

import (
	"fmt"
	"strings"

	"github.com/placeholder/compose/internal/config"
)

// Inputs holds each input file's real path. Music == "" means no
// background music (--no-music).
type Inputs struct {
	Background     string
	Voice          string
	PortraitCutout string
	Music          string
	Logo           string
}

// inputSet assigns ffmpeg input indices in the order inputs are actually
// added — rather than hardcoding index numbers, so toggling an optional
// input (music) never silently shifts every later reference.
type inputSet struct {
	args    []string
	indices map[string]int
	next    int
}

func newInputSet() *inputSet {
	return &inputSet{indices: map[string]int{}}
}

func (s *inputSet) add(name string, path string, extraArgsBeforeInput ...string) int {
	idx := s.next
	s.indices[name] = idx
	s.args = append(s.args, extraArgsBeforeInput...)
	s.args = append(s.args, "-i", path)
	s.next++
	return idx
}

func (s *inputSet) v(name string) string { return fmt.Sprintf("[%d:v]", s.indices[name]) }
func (s *inputSet) a(name string) string { return fmt.Sprintf("[%d:a]", s.indices[name]) }

// BuildOptions controls what BuildCommand assembles — the composition's
// content-independent parameters (layout, dimensions, which optional
// layers are on).
type BuildOptions struct {
	Width, Height, FPS int
	Layout             config.Layout
	IncludeMusic       bool
	IncludeSubs        bool
	// IncludeLogo overlays the logo top-left. Off when no logo_file is
	// configured (the channel runs without a logo) — the caller sets this
	// from cfg.LogoFile being non-empty.
	IncludeLogo bool
	// CaptionTrackPath is the ffconcat list (see
	// subtitles.WriteTrack) of full-frame rounded-card PNGs — added as a
	// concat-demuxer INPUT and overlaid, not embedded as a filter path, so
	// there's no filter-path escaping to get wrong. Required if IncludeSubs.
	CaptionTrackPath string
	// DurationSeconds caps total output length — always voice.wav's real
	// duration, or the --preview value if it's shorter. 0 means
	// unbounded (not used in practice; Build always sets this from a real
	// probed duration).
	DurationSeconds float64
}

// BuildFilterComplex renders the -filter_complex graph string per the
// spec's layer order (bottom to top): blurred/darkened background,
// vignette, portrait cutout, audio-reactive waveform, subtitles, and an
// optional logo — plus the audio ducking chain when music is included.
// Returns the graph string, the video output pad name (whichever layer
// ended up last — "vout" with a logo, else the caption/waveform stage),
// and the audio output pad name (or "" if IncludeMusic is false — the
// caller maps voice's own input directly then, no filter needed for a
// passthrough).
// blurEnabled reports whether a boxblur pass should run — an empty string
// or an explicit "none"/"0" means "leave the background sharp".
func blurEnabled(blur string) bool {
	switch strings.TrimSpace(blur) {
	case "", "none", "0":
		return false
	default:
		return true
	}
}

// vignetteEnabled reports whether a vignette pass should run — an empty
// string or "none" leaves the frame's corners untouched.
func vignetteEnabled(angle string) bool {
	switch strings.TrimSpace(angle) {
	case "", "none":
		return false
	default:
		return true
	}
}

func BuildFilterComplex(in Inputs, ins *inputSet, opts BuildOptions) (graph, videoOut, audioOut string) {
	l := opts.Layout
	var parts []string

	// 1: background — scale to output size, then only the treatments that
	// are actually configured. Blur (empty/"none"/"0") and brightness (0)
	// each skip their filter, so with both off the background plays exactly
	// as generated behind the portrait.
	bg := ins.v("background")
	bgChain := fmt.Sprintf("%sscale=%d:%d", bg, opts.Width, opts.Height)
	if blurEnabled(l.Background.Blur) {
		bgChain += fmt.Sprintf(",boxblur=%s", l.Background.Blur)
	}
	if l.Background.Brightness != 0 {
		bgChain += fmt.Sprintf(",eq=brightness=%g", l.Background.Brightness)
	}
	bgChain += "[bg]"
	parts = append(parts, bgChain)
	bgLabel := "bg"

	// 2: vignette (optional — empty/"none" leaves the frame's corners as-is).
	if vignetteEnabled(l.Vignette.Angle) {
		parts = append(parts, fmt.Sprintf("[bg]vignette=%s[bgv]", l.Vignette.Angle))
		bgLabel = "bgv"
	}

	// 3: portrait cutout — scale to configured height, width auto
	// (-1 preserves aspect ratio), then overlay bottom-left.
	portrait := ins.v("portrait")
	parts = append(parts, fmt.Sprintf("%sscale=-1:%d[portrait]", portrait, l.Portrait.Height))
	parts = append(parts, fmt.Sprintf("[%s][portrait]overlay=x=%d:y=H-h[withportrait]", bgLabel, l.Portrait.X))

	// 4: waveform — drawn from the REAL voice track (showwaves is an
	// audio->video filter; ins.a("voice") is the same stream that
	// actually plays, so sync is automatic, not something to compute). The
	// volume= pre-amplifies only THIS tap of the voice (ffmpeg auto-splits
	// the input, so the audio chain's own use of the same stream is
	// unaffected), and scale lifts quiet passages — together they make the
	// waveform clearly visible instead of a faint speech trace.
	voiceA := ins.a("voice")
	parts = append(parts, fmt.Sprintf("%svolume=%g,showwaves=s=%dx%d:mode=%s:colors=%s:rate=%d:scale=%s:draw=%s[wave]",
		voiceA, l.Waveform.Gain, l.Waveform.Width, l.Waveform.Height, l.Waveform.Mode, l.Waveform.Color, l.Waveform.Rate, l.Waveform.Scale, l.Waveform.Draw))
	waveY := l.WaveformY(opts.Height)
	parts = append(parts, fmt.Sprintf("[withportrait][wave]overlay=x=(W-w)/2:y=%d[withwave]", waveY))

	videoLabel := "withwave"

	// 5: captions (optional — --no-subs skips this step entirely). The
	// caption track is a concat-demuxer video input of full-frame rounded-
	// card PNGs (transparent between cards); format=rgba keeps its alpha,
	// then one overlay burns the whole timed track in at once.
	if opts.IncludeSubs {
		cap := ins.v("captions")
		parts = append(parts, fmt.Sprintf("%sformat=rgba[cap]", cap))
		parts = append(parts, fmt.Sprintf("[%s][cap]overlay=x=0:y=0:eof_action=pass[withsubs]", videoLabel))
		videoLabel = "withsubs"
	}

	// 6: logo, top-left (optional — no logo_file configured skips it, and
	// the last layer built above becomes the final video output directly).
	if opts.IncludeLogo {
		logo := ins.v("logo")
		parts = append(parts, fmt.Sprintf("%sscale=-1:%d[logo]", logo, l.Logo.Height))
		parts = append(parts, fmt.Sprintf("[%s][logo]overlay=x=%d:y=%d[vout]", videoLabel, l.Logo.X, l.Logo.Y))
		videoLabel = "vout"
	}

	// Audio: sidechain-ducked music under the voice, or — with no music —
	// the voice passed through the graph. The passthrough matters: the voice
	// input [1:a] is already consumed by showwaves above, and ffmpeg forbids
	// also -map'ing that same raw input directly, so we route it through an
	// anull to get a proper output pad ([audio]) to map. (When music is on,
	// amix already produces that pad.)
	audioOutLabel := "audio"
	if opts.IncludeMusic {
		music := ins.a("music")
		m := l.Music
		parts = append(parts, fmt.Sprintf("%saloop=loop=-1:size=2e9,volume=%g[m]", music, m.Volume))
		parts = append(parts, fmt.Sprintf("[m]%ssidechaincompress=threshold=%g:ratio=%g:attack=%d:release=%d[ducked]",
			voiceA, m.DuckThreshold, m.DuckRatio, m.DuckAttack, m.DuckRelease))
		parts = append(parts, fmt.Sprintf("[ducked]%samix=inputs=2:duration=longest[audio]", voiceA))
	} else {
		parts = append(parts, fmt.Sprintf("%sanull[audio]", voiceA))
	}

	return strings.Join(parts, ";\n"), videoLabel, audioOutLabel
}
