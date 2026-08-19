package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeBundle(t *testing.T, dir, text, manifestJSON string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "script.txt"), []byte(text), 0o644); err != nil {
		t.Fatalf("write script.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatalf("write manifest.json: %v", err)
	}
}

func validManifestJSON() string {
	return `{
		"id": "script-1",
		"title": "The Ledger",
		"word_count": 4,
		"narrator": {"name": "Clara Vance", "age": 43, "sex": "female"},
		"chapters": [
			{"index": 1, "beat": "hook", "words": 4, "char_start": 0, "char_end": 12}
		]
	}`
}

func TestLoadValidBundle(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "hello world.", validManifestJSON())

	b, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Manifest.ID != "script-1" || b.Manifest.Narrator.Name != "Clara Vance" {
		t.Fatalf("unexpected manifest: %+v", b.Manifest)
	}
	if b.Text != "hello world." {
		t.Fatalf("unexpected text: %q", b.Text)
	}
}

func TestLoadMissingScriptTextReturnsErrNotFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(validManifestJSON()), 0o644); err != nil {
		t.Fatalf("write manifest.json: %v", err)
	}

	_, err := Load(dir)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadMissingManifestReturnsErrNotFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "script.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write script.txt: %v", err)
	}

	_, err := Load(dir)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadMissingDirReturnsErrNotFound(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadMalformedJSONReturnsErrInvalidNotErrNotFound(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "hello world.", `{not valid json`)

	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed JSON should be ErrInvalid, not ErrNotFound, got %v", err)
	}
}

func TestLoadChapterRangeOutsideTextReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	// script.txt is only 5 bytes but the manifest claims a chapter ending at 12.
	writeBundle(t, dir, "short", validManifestJSON())

	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for an out-of-range chapter, got %v", err)
	}
}

func TestLoadMissingNarratorNameReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "hello world.", `{
		"id": "script-1", "title": "T", "word_count": 2,
		"narrator": {"name": "", "age": 0, "sex": ""},
		"chapters": [{"index": 1, "beat": "hook", "words": 2, "char_start": 0, "char_end": 12}]
	}`)

	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a missing narrator name, got %v", err)
	}
}

func TestLoadZeroWordCountReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "hello world.", `{
		"id": "script-1", "title": "T", "word_count": 0,
		"narrator": {"name": "Clara Vance", "age": 43, "sex": "female"},
		"chapters": [{"index": 1, "beat": "hook", "words": 2, "char_start": 0, "char_end": 12}]
	}`)

	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for word_count 0, got %v", err)
	}
}

func TestLoadNoChaptersReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "hello world.", `{
		"id": "script-1", "title": "T", "word_count": 2,
		"narrator": {"name": "Clara Vance", "age": 43, "sex": "female"},
		"chapters": []
	}`)

	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for no chapters, got %v", err)
	}
}
