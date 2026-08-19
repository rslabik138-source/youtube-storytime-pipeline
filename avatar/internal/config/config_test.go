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
gemini_model: gemini-2.5-flash-image
imagen_model: imagen-4.0-fast-generate-001
aspect_ratio: "16:9"
max_cost_usd: 0.50
output_dir: output
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings.yaml: %v", err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.APIKeyEnv != "GOOGLE_AI_STUDIO_API_KEY" || s.GeminiModel != "gemini-2.5-flash-image" {
		t.Fatalf("unexpected settings: %+v", s)
	}
	if s.AspectRatio != "16:9" || s.MaxCostUSD != 0.50 {
		t.Fatalf("unexpected settings: %+v", s)
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
	if s.GeminiModel == "" || s.ImagenModel == "" {
		t.Fatalf("expected default model names, got gemini=%q imagen=%q", s.GeminiModel, s.ImagenModel)
	}
	if s.AspectRatio != "16:9" {
		t.Fatalf("expected default aspect_ratio 16:9, got %q", s.AspectRatio)
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
	if !s.Cutout.IsEnabled() {
		t.Fatalf("expected cutout enabled by default")
	}
	if s.Cutout.RembgCmd != "rembg" || s.Cutout.Model != "birefnet-portrait" {
		t.Fatalf("unexpected cutout defaults: %+v", s.Cutout)
	}
}

func TestLoadCutoutCanBeDisabledExplicitly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte("cutout:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatalf("write settings.yaml: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Cutout.IsEnabled() {
		t.Fatalf("expected explicit `enabled: false` to disable cutout")
	}
	// Even disabled, the command/model still get their defaults so a later
	// re-enable needs no other edits.
	if s.Cutout.RembgCmd != "rembg" || s.Cutout.Model != "birefnet-portrait" {
		t.Fatalf("expected cutout cmd/model defaults even when disabled: %+v", s.Cutout)
	}
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}
