package imagegen

import (
	"context"
	"strings"
	"testing"
)

func TestFailoverUsesPrimaryWhenItSucceeds(t *testing.T) {
	primary := NewFakeGenerator("gemini", "gemini-model")
	fallback := NewFakeGenerator("imagen", "imagen-model")
	f := NewFailover(primary, fallback)

	img, err := f.Generate(context.Background(), "a portrait", Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if img.Provider != "gemini" {
		t.Fatalf("expected the primary provider to serve the request, got %q", img.Provider)
	}
}

func TestFailoverFallsBackWhenPrimaryFails(t *testing.T) {
	primary := &FakeGenerator{Provider: "gemini", Model: "gemini-model", FailOn: "portrait"}
	fallback := NewFakeGenerator("imagen", "imagen-model")
	f := NewFailover(primary, fallback)

	img, err := f.Generate(context.Background(), "a portrait", Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if img.Provider != "imagen" {
		t.Fatalf("expected the fallback provider to serve the request after the primary failed, got %q", img.Provider)
	}
}

func TestFailoverAllFailReturnsCombinedError(t *testing.T) {
	primary := &FakeGenerator{Provider: "gemini", Model: "gemini-model", FailOn: "portrait"}
	fallback := &FakeGenerator{Provider: "imagen", Model: "imagen-model", FailOn: "portrait"}
	f := NewFailover(primary, fallback)

	_, err := f.Generate(context.Background(), "a portrait", Options{})
	if err == nil {
		t.Fatalf("expected an error when every provider fails")
	}
	if !strings.Contains(err.Error(), "gemini") || !strings.Contains(err.Error(), "imagen") {
		t.Fatalf("expected the combined error to mention both providers, got: %v", err)
	}
}

func TestFailoverNoGeneratorsReturnsError(t *testing.T) {
	f := NewFailover()
	_, err := f.Generate(context.Background(), "x", Options{})
	if err == nil {
		t.Fatalf("expected an error for an empty generator list")
	}
}

func TestFailoverRespectsContextCancellation(t *testing.T) {
	primary := &FakeGenerator{Provider: "gemini", Model: "gemini-model", FailOn: "portrait"}
	fallback := NewFakeGenerator("imagen", "imagen-model")
	f := NewFailover(primary, fallback)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.Generate(ctx, "a portrait", Options{})
	if err == nil {
		t.Fatalf("expected an error for an already-cancelled context")
	}
}
