package subtitles

import (
	"image"
	"image/color"
	"testing"
)

func testCardStyle() CardStyle {
	return CardStyle{
		FontSize:     54,
		TextColor:    color.RGBA{R: 255, G: 255, B: 255, A: 255},
		BoxColor:     color.RGBA{R: 0, G: 0, B: 0, A: 217}, // ~85% opaque
		CornerRadius: 20,
		PaddingX:     32,
		PaddingY:     20,
		LineSpacing:  1.15,
		MaxTextWidth: 760,
		ZoneLeft:     950,
		ZoneRight:    60,
	}
}

func TestRenderCardFrameProducesFullFrameImage(t *testing.T) {
	img, err := RenderCardFrame("hello there", testCardStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("RenderCardFrame: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 1920 || b.Dy() != 1080 {
		t.Fatalf("expected a full 1920x1080 frame, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestRenderCardFrameCardSitsInTheRightZoneAndCornersAreTransparent(t *testing.T) {
	img, err := RenderCardFrame("hello there", testCardStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("RenderCardFrame: %v", err)
	}
	rgba := toNRGBA(img)

	// Top-left corner (where a bottom-left portrait would be) must be fully
	// transparent — the card must not stray there.
	if _, _, _, a := rgba.At(10, 10).RGBA(); a != 0 {
		t.Fatalf("expected the top-left corner to be transparent, got alpha %d", a>>8)
	}

	// Somewhere in the right-side zone, vertically centered, there must be
	// opaque-ish card pixels.
	found := false
	for x := 950; x < 1900 && !found; x += 5 {
		if _, _, _, a := rgba.At(x, 540).RGBA(); a>>8 > 100 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the card to render (non-transparent pixels) in the right-side zone at mid-height")
	}
}

func TestRenderCardFrameFallsBackToEmbeddedFontWhenNoneGiven(t *testing.T) {
	style := testCardStyle()
	style.FontBytes = nil // force the embedded-font fallback
	if _, err := RenderCardFrame("some text", style, 1280, 720); err != nil {
		t.Fatalf("expected the embedded font fallback to work, got: %v", err)
	}
}

func TestDefaultFontBytesIsNonEmpty(t *testing.T) {
	if len(DefaultFontBytes()) == 0 {
		t.Fatalf("expected an embedded default font")
	}
}

// toNRGBA converts to a straight-alpha NRGBA so per-pixel alpha reads back
// the way the renderer wrote it (image.RGBA from gg is premultiplied).
func toNRGBA(img image.Image) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}
