// Package imagegen generates a single portrait image from a text prompt,
// via a real provider (Gemini, Imagen) or FakeGenerator for tests.
package imagegen

import "context"

// Options controls image generation.
type Options struct {
	// AspectRatio is e.g. "16:9" or "1:1" — sized with crop room for video,
	// per settings.yaml.
	AspectRatio string
}

// Image is one generated image plus which provider/model actually served
// it — cost is computed separately (see internal/config.Pricing), not
// carried on this type, since a client shouldn't need to know pricing to
// generate an image.
type Image struct {
	PNG      []byte
	Provider string
	Model    string
}

// Generator is the interface both real clients and FakeGenerator
// implement — callers depend on this, never on a concrete client type, so
// tests never need a real API key or network access.
type Generator interface {
	Generate(ctx context.Context, prompt string, opts Options) (Image, error)
}
