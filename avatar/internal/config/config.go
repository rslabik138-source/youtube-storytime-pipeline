// Package config loads settings.yaml (provider/model selection, aspect
// ratio, cost limit) and pricing.yaml (cost per image by model).
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Settings is settings.yaml's shape. Both Gemini and Imagen are accessed
// through the same Google AI Studio API key (confirmed already configured
// for the scenario module), so there's one api_key_env, not two.
type Settings struct {
	APIKeyEnv   string  `yaml:"api_key_env"`
	BaseURL     string  `yaml:"base_url"`
	GeminiModel string  `yaml:"gemini_model"`
	ImagenModel string  `yaml:"imagen_model"`
	AspectRatio string  `yaml:"aspect_ratio"`
	MaxCostUSD  float64 `yaml:"max_cost_usd"`
	OutputDir   string  `yaml:"output_dir"`
	// ScenarioBundleDir is where scenario's `gen export --format bundle`
	// writes each script's <id>/ directory — the ONLY thing avatar reads
	// from the scenario module (see internal/manifest). Defaults to the
	// sibling module's output dir, matching go.work's layout.
	ScenarioBundleDir string `yaml:"scenario_bundle_dir"`
	// Cutout controls background removal — the generated portrait is matted
	// to a transparent PNG (see internal/cutout) so it drops straight into
	// compose as the portrait cutout.
	Cutout CutoutSettings `yaml:"cutout"`
}

// CutoutSettings configures the rembg background-removal step.
type CutoutSettings struct {
	// Enabled runs the cutout after generation. Defaults to true; set it
	// explicitly to false to write the raw opaque image as-is (useful for
	// debugging the generation prompt). A pointer so an omitted key defaults
	// to on while an explicit `enabled: false` still turns it off.
	Enabled *bool `yaml:"enabled"`
	// RembgCmd is the rembg executable — a bare "rembg" (on PATH) or a full
	// path to rembg.exe when it isn't (common with a user-scope Python
	// install on Windows).
	RembgCmd string `yaml:"rembg_cmd"`
	// Model is the rembg model. "birefnet-portrait" gives the best hair
	// edges for a human head-and-shoulders shot.
	Model string `yaml:"model"`
}

// IsEnabled reports whether the cutout step should run (an omitted key
// defaults to on — see applyDefaults).
func (c CutoutSettings) IsEnabled() bool { return c.Enabled != nil && *c.Enabled }

// Load reads and parses settings.yaml at path, filling in defaults for
// every zero-value field.
func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	var s Settings
	if err := yaml.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	s.applyDefaults()
	return s, nil
}

func (s *Settings) applyDefaults() {
	if s.APIKeyEnv == "" {
		s.APIKeyEnv = "GOOGLE_AI_STUDIO_API_KEY"
	}
	if s.BaseURL == "" {
		s.BaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"
	}
	if s.GeminiModel == "" {
		s.GeminiModel = "gemini-2.5-flash-image"
	}
	if s.ImagenModel == "" {
		s.ImagenModel = "imagen-4.0-fast-generate-001"
	}
	if s.AspectRatio == "" {
		s.AspectRatio = "16:9"
	}
	if s.MaxCostUSD <= 0 {
		s.MaxCostUSD = 0.50
	}
	if s.OutputDir == "" {
		s.OutputDir = "output"
	}
	if s.ScenarioBundleDir == "" {
		s.ScenarioBundleDir = "../scenario/output/scripts"
	}
	if s.Cutout.Enabled == nil {
		on := true
		s.Cutout.Enabled = &on
	}
	if s.Cutout.RembgCmd == "" {
		s.Cutout.RembgCmd = "rembg"
	}
	if s.Cutout.Model == "" {
		s.Cutout.Model = "birefnet-portrait"
	}
}
