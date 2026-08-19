package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
)

// MobileThumbW and MobileThumbH are the size a YouTube thumbnail occupies
// in a phone feed — the acid test for whether the composition has too many
// words to read. A thumbnail that's illegible here is illegible where most
// viewers actually see it.
const (
	MobileThumbW = 168
	MobileThumbH = 94
)

// MobileCheck downscales a rendered 1280x720 thumbnail PNG to phone-feed
// size and scores how legible its text zone still is. It returns the
// downscaled PNG (so a human can eyeball it) and a contrast score: the RMS
// contrast (standard deviation of luma, 0-255) over the left 66% where the
// text lives. Big, high-stroke text keeps a high stddev after downscaling;
// text that's shrunk to too many small words averages toward its
// background and the score collapses. The caller decides the pass line —
// this only measures.
func MobileCheck(pngBytes []byte) (mobilePNG []byte, contrast float64, err error) {
	src, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("render: mobile decode: %w", err)
	}
	small := downscaleArea(src, MobileThumbW, MobileThumbH)

	var buf bytes.Buffer
	if err := png.Encode(&buf, small); err != nil {
		return nil, 0, fmt.Errorf("render: mobile encode: %w", err)
	}
	return buf.Bytes(), textZoneContrast(small, 0.66), nil
}

// downscaleArea box-averages src down to w x h — a plain area filter, good
// enough for a legibility proxy and dependency-free (no x/image/draw).
func downscaleArea(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for dy := 0; dy < h; dy++ {
		y0 := b.Min.Y + dy*sh/h
		y1 := b.Min.Y + (dy+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < w; dx++ {
			x0 := b.Min.X + dx*sw/w
			x1 := b.Min.X + (dx+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var rs, gs, bs, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, bl, _ := src.At(x, y).RGBA()
					rs += uint64(r >> 8)
					gs += uint64(g >> 8)
					bs += uint64(bl >> 8)
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			dst.Set(dx, dy, color.RGBA{uint8(rs / n), uint8(gs / n), uint8(bs / n), 255})
		}
	}
	return dst
}

// textZoneContrast returns the standard deviation of luma over the left
// widthFrac of img — the RMS-contrast legibility proxy MobileCheck reports.
func textZoneContrast(img *image.RGBA, widthFrac float64) float64 {
	b := img.Bounds()
	xEnd := b.Min.X + int(float64(b.Dx())*widthFrac)
	var sum, sumSq, n float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < xEnd; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			luma := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
			sum += luma
			sumSq += luma * luma
			n++
		}
	}
	if n == 0 {
		return 0
	}
	mean := sum / n
	variance := sumSq/n - mean*mean
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}
