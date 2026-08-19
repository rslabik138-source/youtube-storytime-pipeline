package render

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
)

// FakeRenderer is a Renderer that never launches a browser: it records
// every HTML string it was asked to render and returns a real, valid,
// decodable PNG (or Err, if set) — no Chrome install needed for
// `go test ./...`.
type FakeRenderer struct {
	HTMLReceived []string
	Err          error
}

func (f *FakeRenderer) Render(ctx context.Context, html string) ([]byte, error) {
	f.HTMLReceived = append(f.HTMLReceived, html)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.Err != nil {
		return nil, f.Err
	}
	return stubPNG(), nil
}

// stubPNG builds a tiny real PNG (8x8, one flat color) — enough for a
// caller to decode and check dimensions/format without a real render.
func stubPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	fill := color.RGBA{R: 200, G: 30, B: 30, A: 255}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // encoding an in-memory 8x8 RGBA image cannot fail
	}
	return buf.Bytes()
}
