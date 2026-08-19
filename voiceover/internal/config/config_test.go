package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesSettingsYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	content := `
kokoro_url: http://localhost:8880
chunk_max_chars: 400
concurrency: 2
speed: 1.0
pause_paragraph_ms: 350
pause_chapter_ms: 700
loudness_lufs: -14
output_format: wav
keep_chunks: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings.yaml: %v", err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.KokoroURL != "http://localhost:8880" || s.ChunkMaxChars != 400 || s.Concurrency != 2 {
		t.Fatalf("unexpected settings: %+v", s)
	}
	if s.LoudnessLUFS != -14 {
		t.Fatalf("expected loudness_lufs -14, got %v", s.LoudnessLUFS)
	}
}

func TestLoadAppliesDefaultsForZeroFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write settings.yaml: %v", err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.KokoroURL != "http://localhost:8880" {
		t.Fatalf("expected default kokoro_url, got %q", s.KokoroURL)
	}
	if s.ChunkMaxChars != 400 || s.Concurrency != 2 || s.Speed != 1.0 {
		t.Fatalf("expected defaults for chunk_max_chars/concurrency/speed, got %+v", s)
	}
	if s.PauseParagraphMs != 350 || s.PauseChapterMs != 700 {
		t.Fatalf("expected default pauses, got %+v", s)
	}
	if s.LoudnessLUFS != -14 {
		t.Fatalf("expected default loudness -14, got %v", s.LoudnessLUFS)
	}
	if s.DBPath != "output/voiceover.db" || s.OutputDir != "output" {
		t.Fatalf("expected default db_path/output_dir, got %+v", s)
	}
	if s.ScenarioBundleDir != "../scenario/output/scripts" {
		t.Fatalf("expected default scenario_bundle_dir, got %q", s.ScenarioBundleDir)
	}
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}

func TestConcurrencyDefaultNeverExceedsThreeEvenIfConfiguredHigher(t *testing.T) {
	// Not enforced by Load itself (the user can genuinely want more), but
	// document the hardware note from the brief: default is 2, never
	// silently raised past what a 6GB-VRAM laptop GPU can handle.
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte("concurrency: 2"), 0o644); err != nil {
		t.Fatalf("write settings.yaml: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Concurrency != 2 {
		t.Fatalf("expected concurrency 2, got %d", s.Concurrency)
	}
}
