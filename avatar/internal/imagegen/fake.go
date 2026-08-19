package imagegen

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// FakeGenerator returns a small, real, valid PNG — no network call — for
// tests that need plausible image bytes without Gemini/Imagen or an API
// key.
type FakeGenerator struct {
	Provider string
	Model    string
	// FailOn, if non-empty, makes Generate return an error whenever prompt
	// contains this substring — for testing a caller's error handling
	// (e.g. Failover falling through to the next provider).
	FailOn string
}

// NewFakeGenerator builds a FakeGenerator reporting the given
// provider/model on every successful Image.
func NewFakeGenerator(provider, model string) *FakeGenerator {
	return &FakeGenerator{Provider: provider, Model: model}
}

func (f *FakeGenerator) Generate(ctx context.Context, prompt string, opts Options) (Image, error) {
	if err := ctx.Err(); err != nil {
		return Image{}, err
	}
	if f.FailOn != "" && strings.Contains(prompt, f.FailOn) {
		return Image{}, fmt.Errorf("fake imagegen (%s): forced failure for prompt containing %q", f.Provider, f.FailOn)
	}

	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 128, G: 128, B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return Image{}, fmt.Errorf("fake imagegen: encode png: %w", err)
	}
	return Image{PNG: buf.Bytes(), Provider: f.Provider, Model: f.Model}, nil
}
