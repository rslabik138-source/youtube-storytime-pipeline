// Package config loads settings.yaml (LLM provider/model, output/scenario
// paths, chromedp render options, badge), faces.yaml (the channel's
// standing portrait library), and pricing.yaml (cost per text-generation
// call by model).
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// BadgeConfig is the optional channel-branding badge rendered bottom-left
// on every thumbnail. Disabled (zero value) unless settings.yaml turns it
// on — not every channel wants one, and a badge with no Text would just be
// an empty box.
type BadgeConfig struct {
	Enabled bool   `yaml:"enabled"`
	Text    string `yaml:"text"`
}

// Settings is settings.yaml's shape.
type Settings struct {
	APIKeyEnv string `yaml:"api_key_env"`
	BaseURL   string `yaml:"base_url"`
	// TextModel generates the thumbnail's lines — deliberately a cheap
	// model (see prompts/thumbnail.tmpl's own brief: this is a short,
	// structured JSON call, not prose).
	TextModel  string  `yaml:"text_model"`
	MaxCostUSD float64 `yaml:"max_cost_usd"`
	OutputDir  string  `yaml:"output_dir"`
	// ScenarioBundleDir is where scenario's `gen export --format bundle`
	// writes each script's <id>/ directory — the ONLY thing thumbnail reads
	// from the scenario module (see internal/manifest). Defaults to the
	// sibling module's output dir, matching go.work's layout.
	ScenarioBundleDir string `yaml:"scenario_bundle_dir"`
	// ChromePath overrides chromedp's own executable auto-detection —
	// leave blank to let chromedp find an installed Chrome/Chromium/Edge
	// itself; set explicitly if that detection picks the wrong one or
	// finds nothing.
	ChromePath string `yaml:"chrome_path"`
	// RenderTimeoutSec bounds one chromedp render (page load + font-fit
	// script + screenshot) — a hung headless Chrome must not hang the CLI
	// forever.
	RenderTimeoutSec int         `yaml:"render_timeout_sec"`
	Badge            BadgeConfig `yaml:"badge"`
}

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
	if s.TextModel == "" {
		s.TextModel = "gemini-3.5-flash-lite"
	}
	if s.MaxCostUSD <= 0 {
		s.MaxCostUSD = 0.05
	}
	if s.OutputDir == "" {
		s.OutputDir = "output"
	}
	if s.ScenarioBundleDir == "" {
		s.ScenarioBundleDir = "../scenario/output/scripts"
	}
	if s.RenderTimeoutSec <= 0 {
		s.RenderTimeoutSec = 30
	}
}
