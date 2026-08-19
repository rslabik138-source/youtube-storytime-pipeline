package subtitles

import (
	"fmt"
	"image"
	"image/color"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
)

// CardStyle controls how one caption card is drawn — everything the
// rounded-card look needs, all resolved from config.Layout by the caller
// (colors as real color.Color, not ASS &H strings, since this is a Go
// renderer now, not libass).
type CardStyle struct {
	// FontBytes is TTF font data. Empty falls back to DefaultFontBytes()
	// (embedded Go Bold — a real bold sans-serif needing no external
	// file). Point it at a Montserrat SemiBold .ttf for the intended look.
	FontBytes    []byte
	FontSize     float64
	TextColor    color.RGBA // straight (non-premultiplied) alpha
	BoxColor     color.RGBA // straight alpha — the card's fill, A carries opacity
	CornerRadius float64
	PaddingX     float64
	PaddingY     float64
	LineSpacing  float64 // line-advance as a multiple of FontSize; ~1.15 reads well
	MaxTextWidth float64 // wrap text to at most this width (px) before the card grows

	// The card is centered horizontally within [ZoneLeft, frameW-ZoneRight]
	// and centered vertically in the frame — ZoneLeft big enough clears a
	// bottom-left portrait (this replaces the ASS MarginL trick).
	ZoneLeft  int
	ZoneRight int
}

// DefaultFontBytes returns the embedded Go Bold TTF — always available, no
// external file. Used when CardStyle.FontBytes is empty.
func DefaultFontBytes() []byte { return gobold.TTF }

// setStraightRGBA applies c to dc as a straight-alpha color — see the
// call site's comment for why SetColor is wrong for a semi-transparent
// color.RGBA.
func setStraightRGBA(dc *gg.Context, c color.RGBA) {
	dc.SetRGBA(float64(c.R)/255, float64(c.G)/255, float64(c.B)/255, float64(c.A)/255)
}

func (s CardStyle) fontFace() (font.Face, error) {
	data := s.FontBytes
	if len(data) == 0 {
		data = DefaultFontBytes()
	}
	f, err := truetype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("subtitles: parse font: %w", err)
	}
	return truetype.NewFace(f, &truetype.Options{Size: s.FontSize}), nil
}

// RenderCardFrame draws one caption as a FULL-FRAME (frameW x frameH) RGBA
// image: transparent everywhere except a rounded card, positioned in the
// right-side zone, with text wrapped inside it. Full-frame (rather than a
// tight card image) is deliberate — every caption is the same size, so the
// whole caption track can be concatenated into one video stream and
// overlaid in a single ffmpeg step (see WriteTrack), instead of one input
// and one overlay filter per line.
func RenderCardFrame(text string, style CardStyle, frameW, frameH int) (image.Image, error) {
	face, err := style.fontFace()
	if err != nil {
		return nil, err
	}

	// A throwaway context just to measure/wrap with this face.
	measure := gg.NewContext(1, 1)
	measure.SetFontFace(face)
	rows := measure.WordWrap(text, style.MaxTextWidth)
	if len(rows) == 0 {
		rows = []string{text}
	}

	lineHeight := style.FontSize * style.LineSpacing
	textW := 0.0
	for _, r := range rows {
		w, _ := measure.MeasureString(r)
		if w > textW {
			textW = w
		}
	}
	// Snug block height: one line hugs the glyphs (FontSize); each EXTRA line
	// adds a full lineHeight. Using lineHeight*len(rows) would tack on an extra
	// (LineSpacing-1)*FontSize of dead space, making a single-line card look
	// too tall for its text.
	textH := style.FontSize + lineHeight*float64(len(rows)-1)

	cardW := textW + 2*style.PaddingX
	cardH := textH + 2*style.PaddingY

	zoneW := float64(frameW - style.ZoneRight - style.ZoneLeft)
	cardX := float64(style.ZoneLeft) + (zoneW-cardW)/2
	if cardX < float64(style.ZoneLeft) {
		cardX = float64(style.ZoneLeft) // a card wider than the zone still starts at the zone's left edge, not off-frame
	}
	cardY := (float64(frameH) - cardH) / 2

	dc := gg.NewContext(frameW, frameH) // starts fully transparent
	dc.SetFontFace(face)

	// SetRGBA (straight alpha), not SetColor: gg's SetColor calls
	// color.RGBA's premultiplied RGBA(), which mis-renders a straight-alpha
	// color.RGBA where R > A (e.g. opaque-white-at-85%).
	dc.DrawRoundedRectangle(cardX, cardY, cardW, cardH, style.CornerRadius)
	setStraightRGBA(dc, style.BoxColor)
	dc.Fill()

	setStraightRGBA(dc, style.TextColor)
	// Each row centered horizontally; rows stepped by lineHeight. The first
	// row's vertical center sits PaddingY + half a glyph below the card top,
	// so with the snug textH above the top and bottom padding read equal.
	// DrawStringAnchored's ay=0.5 centers each row around its y.
	firstCenterY := cardY + style.PaddingY + style.FontSize/2
	centerX := cardX + cardW/2
	for i, r := range rows {
		y := firstCenterY + lineHeight*float64(i)
		dc.DrawStringAnchored(r, centerX, y, 0.5, 0.5)
	}

	return dc.Image(), nil
}
