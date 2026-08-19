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
api_key_env: GOOGLE_AI_STUDIO_API_KEY
base_url: https://generativelanguage.googleapis.com/v1beta/openai
text_model: gemini-3.5-flash-lite
max_cost_usd: 0.05
output_dir: output
badge:
  enabled: true
  text: underestimated
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings.yaml: %v", err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.APIKeyEnv != "GOOGLE_AI_STUDIO_API_KEY" || s.TextModel != "gemini-3.5-flash-lite" {
		t.Fatalf("unexpected settings: %+v", s)
	}
	if s.MaxCostUSD != 0.05 {
		t.Fatalf("unexpected max_cost_usd: %+v", s)
	}
	if !s.Badge.Enabled || s.Badge.Text != "underestimated" {
		t.Fatalf("unexpected badge config: %+v", s.Badge)
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
	if s.APIKeyEnv != "GOOGLE_AI_STUDIO_API_KEY" {
		t.Fatalf("expected default api_key_env, got %q", s.APIKeyEnv)
	}
	if s.TextModel == "" {
		t.Fatalf("expected a default text_model")
	}
	if s.MaxCostUSD <= 0 {
		t.Fatalf("expected a positive default max_cost_usd, got %v", s.MaxCostUSD)
	}
	if s.OutputDir != "output" {
		t.Fatalf("expected default output_dir, got %q", s.OutputDir)
	}
	if s.ScenarioBundleDir != "../scenario/output/scripts" {
		t.Fatalf("expected default scenario_bundle_dir, got %q", s.ScenarioBundleDir)
	}
	if s.RenderTimeoutSec <= 0 {
		t.Fatalf("expected a positive default render_timeout_sec, got %v", s.RenderTimeoutSec)
	}
	if s.Badge.Enabled {
		t.Fatalf("expected badge disabled by default")
	}
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}
