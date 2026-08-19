package ffmpeg

import (
	"context"
	"strings"
	"testing"
)

func TestRunWithFallbackUsesPrimaryWhenItSucceeds(t *testing.T) {
	runner := &FakeRunner{}
	primary := Encoder{Name: "h264_nvenc", Args: []string{"-c:v", "h264_nvenc"}}
	fallback := Encoder{Name: "libx264", Args: []string{"-c:v", "libx264"}}

	used, err := RunWithFallback(context.Background(), runner, func(e Encoder) []string { return e.Args }, primary, fallback, nil)
	if err != nil {
		t.Fatalf("RunWithFallback: %v", err)
	}
	if used.Name != "h264_nvenc" {
		t.Fatalf("expected the primary encoder to be used, got %q", used.Name)
	}
	if len(runner.RunCalls) != 1 {
		t.Fatalf("expected exactly 1 run when the primary succeeds, got %d", len(runner.RunCalls))
	}
}

func TestRunWithFallbackFallsBackWhenPrimaryFails(t *testing.T) {
	runner := &FakeRunner{FailArgsContaining: "h264_nvenc"}
	primary := Encoder{Name: "h264_nvenc", Args: []string{"-c:v", "h264_nvenc"}}
	fallback := Encoder{Name: "libx264", Args: []string{"-c:v", "libx264"}}

	var fellBackFrom error
	used, err := RunWithFallback(context.Background(), runner, func(e Encoder) []string { return e.Args }, primary, fallback, func(primaryErr error) {
		fellBackFrom = primaryErr
	})
	if err != nil {
		t.Fatalf("RunWithFallback: %v", err)
	}
	if used.Name != "libx264" {
		t.Fatalf("expected the fallback encoder to be used, got %q", used.Name)
	}
	if len(runner.RunCalls) != 2 {
		t.Fatalf("expected 2 runs (primary attempt + fallback), got %d", len(runner.RunCalls))
	}
	if fellBackFrom == nil {
		t.Fatalf("expected onFallback to be called with the primary's error")
	}
}

func TestRunWithFallbackReturnsErrorWhenBothFail(t *testing.T) {
	runner := &FakeRunner{FailArgsContaining: "-c:v"} // matches both encoders' args
	primary := Encoder{Name: "h264_nvenc", Args: []string{"-c:v", "h264_nvenc"}}
	fallback := Encoder{Name: "libx264", Args: []string{"-c:v", "libx264"}}

	_, err := RunWithFallback(context.Background(), runner, func(e Encoder) []string { return e.Args }, primary, fallback, nil)
	if err == nil {
		t.Fatalf("expected an error when both encoders fail")
	}
	if !strings.Contains(err.Error(), "h264_nvenc") || !strings.Contains(err.Error(), "libx264") {
		t.Fatalf("expected the error to mention both encoders, got: %v", err)
	}
}
