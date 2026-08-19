package config

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseHexColor turns a "#RRGGBB" (or "RRGGBB") string plus an opacity in
// [0,1] into a straight-alpha color.RGBA. gg draws with straight (non-
// premultiplied) alpha via SetColor, so this returns color.RGBA with A
// scaled from opacity and RGB left un-premultiplied.
func ParseHexColor(hex string, opacity float64) (color.RGBA, error) {
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) != 6 {
		return color.RGBA{}, fmt.Errorf("config: color %q must be #RRGGBB", hex)
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(h, "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{}, fmt.Errorf("config: color %q: %w", hex, err)
	}
	if opacity < 0 {
		opacity = 0
	}
	if opacity > 1 {
		opacity = 1
	}
	return color.RGBA{R: r, G: g, B: b, A: uint8(opacity*255 + 0.5)}, nil
}

// Layout is layout.yaml's shape — every position, size, color, and
// opacity the composition uses, so the look can be retuned without
// touching Go code.
type Layout struct {
	Background struct {
		// Blur is boxblur's own "luma_radius:luma_power" argument string.
		Blur       string  `yaml:"blur"`
		Brightness float64 `yaml:"brightness"`
	} `yaml:"background"`

	Vignette struct {
		Angle string `yaml:"angle"` // vignette filter's own angle expression, e.g. "PI/5"
	} `yaml:"vignette"`

	Portrait struct {
		// Height is the cutout's rendered height in pixels; width scales
		// to preserve aspect ratio. X is a fixed left offset; Y is always
		// bottom-anchored (H-h) — the portrait's feet sit on the frame's
		// bottom edge, not independently configurable, since any other Y
		// would float it off the ground.
		Height int `yaml:"height"`
		X      int `yaml:"x"`
	} `yaml:"portrait"`

	Waveform struct {
		Width  int    `yaml:"width"`
		Height int    `yaml:"height"`
		Mode   string `yaml:"mode"`
		Color  string `yaml:"color"`
		Rate   int    `yaml:"rate"`
		// Draw is showwaves' draw mode. "full" draws the waveform at full
		// intensity — this is the one that actually matters for visibility:
		// the default "scale" dims each column by its own amplitude, which
		// renders as a faint gray that vanishes on a dark background (the
		// original "barely visible" bug). Keep this "full".
		Draw string `yaml:"draw"` // full | scale
		// Gain pre-amplifies ONLY the audio copy fed to showwaves (a
		// separate tap off the voice, not the audio that plays). With
		// draw=full the waveform is already bright at unity gain, so this
		// defaults to 1 (off); raise it for taller peaks. Scale is
		// showwaves' amplitude mapping (lin/log/sqrt/cbrt).
		Gain  float64 `yaml:"gain"`
		Scale string  `yaml:"scale"`
		// BottomMargin is the gap between the waveform's own bottom edge
		// and the frame's bottom edge — Y is derived as H-Height-BottomMargin,
		// not stored directly, so retuning Height alone doesn't silently
		// change how much margin is left.
		BottomMargin int `yaml:"bottom_margin"`
	} `yaml:"waveform"`

	Logo struct {
		X      int `yaml:"x"`
		Y      int `yaml:"y"`
		Height int `yaml:"height"`
	} `yaml:"logo"`

	// Subtitles are rendered as rounded caption cards drawn in Go (see
	// internal/subtitles/card.go) and overlaid by ffmpeg — NOT via libass,
	// which can't round a box's corners. Colors are plain #RRGGBB here, not
	// ASS &H strings.
	Subtitles struct {
		// FontFile points at a .ttf (e.g. Montserrat SemiBold). Empty uses
		// an embedded bold sans-serif (Go Bold) so a render never fails for
		// a missing font.
		FontFile     string  `yaml:"font_file"`
		FontSize     float64 `yaml:"font_size"`
		TextColor    string  `yaml:"text_color"`    // #RRGGBB
		BoxColor     string  `yaml:"box_color"`     // #RRGGBB (opacity is separate)
		BoxOpacity   float64 `yaml:"box_opacity"`   // 0..1
		CornerRadius float64 `yaml:"corner_radius"` // px
		PaddingX     float64 `yaml:"padding_x"`     // px inside the card, left/right of text
		PaddingY     float64 `yaml:"padding_y"`     // px inside the card, above/below text
		LineSpacing  float64 `yaml:"line_spacing"`  // line advance as a multiple of font size
		MaxWidth     float64 `yaml:"max_width"`     // wrap text past this width (px) before the card grows
		// The card centers horizontally within [ZoneLeft, width-ZoneRight]
		// and vertically in the frame — a big ZoneLeft clears a bottom-left
		// portrait (this is the card-era replacement for the old ASS
		// MarginL trick).
		ZoneLeft        int `yaml:"zone_left"`
		ZoneRight       int `yaml:"zone_right"`
		WordsPerLineMin int `yaml:"words_per_line_min"`
		WordsPerLineMax int `yaml:"words_per_line_max"`
	} `yaml:"subtitles"`

	Music struct {
		Volume        float64 `yaml:"volume"`
		DuckThreshold float64 `yaml:"duck_threshold"`
		DuckRatio     float64 `yaml:"duck_ratio"`
		DuckAttack    int     `yaml:"duck_attack"`
		DuckRelease   int     `yaml:"duck_release"`
	} `yaml:"music"`
}

func LoadLayout(path string) (Layout, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Layout{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	var l Layout
	if err := yaml.Unmarshal(data, &l); err != nil {
		return Layout{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	l.applyDefaults()
	return l, nil
}

func (l *Layout) applyDefaults() {
	// Blur, brightness, and vignette intentionally have NO defaults — blank
	// blur / 0 brightness / blank vignette all mean "leave the background as
	// generated". Set them in layout.yaml to treat the background.
	if l.Portrait.Height == 0 {
		l.Portrait.Height = 864
	}
	if l.Portrait.X == 0 {
		l.Portrait.X = 40
	}
	if l.Waveform.Width == 0 {
		l.Waveform.Width = 1536
	}
	if l.Waveform.Height == 0 {
		l.Waveform.Height = 160 // taller than the original 120 for a more prominent waveform
	}
	if l.Waveform.Mode == "" {
		l.Waveform.Mode = "cline"
	}
	if l.Waveform.Color == "" {
		l.Waveform.Color = "white@0.9"
	}
	if l.Waveform.Rate == 0 {
		l.Waveform.Rate = 30
	}
	if l.Waveform.Draw == "" {
		l.Waveform.Draw = "full" // the fix for the "barely visible" waveform — see the field's doc comment
	}
	if l.Waveform.Gain == 0 {
		l.Waveform.Gain = 1 // draw=full is bright at unity; no amplification needed by default
	}
	if l.Waveform.Scale == "" {
		l.Waveform.Scale = "lin"
	}
	if l.Waveform.BottomMargin == 0 {
		l.Waveform.BottomMargin = 20
	}
	if l.Logo.X == 0 {
		l.Logo.X = 40
	}
	if l.Logo.Y == 0 {
		l.Logo.Y = 40
	}
	if l.Logo.Height == 0 {
		l.Logo.Height = 60
	}
	if l.Subtitles.FontSize == 0 {
		l.Subtitles.FontSize = 54
	}
	if l.Subtitles.TextColor == "" {
		l.Subtitles.TextColor = "#FFFFFF"
	}
	if l.Subtitles.BoxColor == "" {
		l.Subtitles.BoxColor = "#000000"
	}
	if l.Subtitles.BoxOpacity == 0 {
		l.Subtitles.BoxOpacity = 0.85 // a real reference read as a near-solid black card
	}
	if l.Subtitles.CornerRadius == 0 {
		l.Subtitles.CornerRadius = 20
	}
	if l.Subtitles.PaddingX == 0 {
		l.Subtitles.PaddingX = 32
	}
	if l.Subtitles.PaddingY == 0 {
		l.Subtitles.PaddingY = 16
	}
	if l.Subtitles.LineSpacing == 0 {
		l.Subtitles.LineSpacing = 1.15
	}
	if l.Subtitles.MaxWidth == 0 {
		l.Subtitles.MaxWidth = 860 // uses more of the right-side zone so fuller lines fit before wrapping
	}
	if l.Subtitles.ZoneLeft == 0 {
		l.Subtitles.ZoneLeft = 950 // clears a bottom-left portrait at the default width/height
	}
	if l.Subtitles.ZoneRight == 0 {
		l.Subtitles.ZoneRight = 60
	}
	if l.Subtitles.WordsPerLineMin == 0 {
		l.Subtitles.WordsPerLineMin = 4
	}
	if l.Subtitles.WordsPerLineMax == 0 {
		l.Subtitles.WordsPerLineMax = 9 // more words per card (was 7) — fewer, fuller cards, less chopping mid-sentence
	}
	if l.Music.Volume == 0 {
		l.Music.Volume = 0.12
	}
	if l.Music.DuckThreshold == 0 {
		l.Music.DuckThreshold = 0.03
	}
	if l.Music.DuckRatio == 0 {
		l.Music.DuckRatio = 8
	}
	if l.Music.DuckAttack == 0 {
		l.Music.DuckAttack = 20
	}
	if l.Music.DuckRelease == 0 {
		l.Music.DuckRelease = 400
	}
}

// WaveformY returns the waveform overlay's Y position — derived from
// Height and BottomMargin, not stored directly (see the struct's doc
// comment). Exposed as a method rather than inlined at each call site so
// the derivation only has one home.
func (l Layout) WaveformY(outputHeight int) int {
	return outputHeight - l.Waveform.Height - l.Waveform.BottomMargin
}
