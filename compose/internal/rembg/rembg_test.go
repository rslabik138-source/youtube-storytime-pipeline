package rembg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writePortrait(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("fake-portrait-png"), 0o644); err != nil {
		t.Fatalf("write portrait: %v", err)
	}
}

func TestEnsureCutoutRunsRembgOnFirstCall(t *testing.T) {
	dir := t.TempDir()
	portrait := filepath.Join(dir, "portrait.png")
	writePortrait(t, portrait)

	runner := &FakeRunner{}
	out, err := EnsureCutout(context.Background(), runner, portrait, dir, "script-1")
	if err != nil {
		t.Fatalf("EnsureCutout: %v", err)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("expected rembg to run once, got %d calls", len(runner.Calls))
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected the cutout file to exist: %v", err)
	}
}

func TestEnsureCutoutSkipsRembgWhenCacheIsFresh(t *testing.T) {
	dir := t.TempDir()
	portrait := filepath.Join(dir, "portrait.png")
	writePortrait(t, portrait)

	runner := &FakeRunner{}
	if _, err := EnsureCutout(context.Background(), runner, portrait, dir, "script-1"); err != nil {
		t.Fatalf("first EnsureCutout: %v", err)
	}
	if _, err := EnsureCutout(context.Background(), runner, portrait, dir, "script-1"); err != nil {
		t.Fatalf("second EnsureCutout: %v", err)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("expected rembg to run exactly once across 2 calls (cache hit on the 2nd), got %d", len(runner.Calls))
	}
}

func TestEnsureCutoutRerunsWhenSourcePortraitIsNewerThanCache(t *testing.T) {
	dir := t.TempDir()
	portrait := filepath.Join(dir, "portrait.png")
	writePortrait(t, portrait)

	runner := &FakeRunner{}
	if _, err := EnsureCutout(context.Background(), runner, portrait, dir, "script-1"); err != nil {
		t.Fatalf("first EnsureCutout: %v", err)
	}

	// Regenerate the portrait (simulates avatar producing a new one) —
	// bump its mtime forward so it's unambiguously newer than the cutout,
	// even on filesystems with coarse mtime resolution.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(portrait, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if _, err := EnsureCutout(context.Background(), runner, portrait, dir, "script-1"); err != nil {
		t.Fatalf("second EnsureCutout: %v", err)
	}
	if len(runner.Calls) != 2 {
		t.Fatalf("expected rembg to re-run once the source portrait changed, got %d calls", len(runner.Calls))
	}
}

func TestEnsureCutoutMissingSourceReturnsError(t *testing.T) {
	dir := t.TempDir()
	runner := &FakeRunner{}
	_, err := EnsureCutout(context.Background(), runner, filepath.Join(dir, "does-not-exist.png"), dir, "script-1")
	if err == nil {
		t.Fatalf("expected an error for a missing source portrait")
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("expected rembg to never run for a missing source, got %d calls", len(runner.Calls))
	}
}

func TestEnsureCutoutPropagatesRunnerError(t *testing.T) {
	dir := t.TempDir()
	portrait := filepath.Join(dir, "portrait.png")
	writePortrait(t, portrait)

	runner := &FakeRunner{Err: errors.New("rembg exploded")}
	if _, err := EnsureCutout(context.Background(), runner, portrait, dir, "script-1"); err == nil {
		t.Fatalf("expected the runner's error to propagate")
	}
}

func TestCutoutPathIsKeyedByID(t *testing.T) {
	p1 := CutoutPath("cache", "id-1")
	p2 := CutoutPath("cache", "id-2")
	if p1 == p2 {
		t.Fatalf("expected different ids to get different cutout paths, both got %q", p1)
	}
}
