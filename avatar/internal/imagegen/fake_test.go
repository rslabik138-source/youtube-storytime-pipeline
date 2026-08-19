package imagegen

import (
	"bytes"
	"context"
	"image/png"
	"testing"
)

func TestFakeGeneratorReturnsValidPNG(t *testing.T) {
	f := NewFakeGenerator("gemini", "test-model")
	img, err := f.Generate(context.Background(), "a portrait", Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if img.Provider != "gemini" || img.Model != "test-model" {
		t.Fatalf("unexpected provider/model: %+v", img)
	}
	if _, err := png.Decode(bytes.NewReader(img.PNG)); err != nil {
		t.Fatalf("expected a valid, decodable PNG, got: %v", err)
	}
}

func TestFakeGeneratorFailOnForcesError(t *testing.T) {
	f := &FakeGenerator{Provider: "gemini", Model: "test-model", FailOn: "BOOM"}
	if _, err := f.Generate(context.Background(), "a prompt with BOOM in it", Options{}); err == nil {
		t.Fatalf("expected an error when prompt contains the FailOn substring")
	}
	if _, err := f.Generate(context.Background(), "a fine prompt", Options{}); err != nil {
		t.Fatalf("expected no error for a prompt without the FailOn substring, got: %v", err)
	}
}

func TestFakeGeneratorRespectsContextCancellation(t *testing.T) {
	f := NewFakeGenerator("gemini", "test-model")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Generate(ctx, "a portrait", Options{}); err == nil {
		t.Fatalf("expected an error for an already-cancelled context")
	}
}
