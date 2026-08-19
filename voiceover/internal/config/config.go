// Package config loads settings.yaml, the one file that drives voiceover's
// runtime behavior (chunking, concurrency, pacing, loudness). The voice
// catalog (voices.yaml) is loaded separately, by internal/catalog.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Settings is settings.yaml's shape.
type Settings struct {
	KokoroURL        string  `yaml:"kokoro_url"`
	ChunkMaxChars    int     `yaml:"chunk_max_chars"`
	Concurrency      int     `yaml:"concurrency"`
	Speed            float64 `yaml:"speed"`
	PauseParagraphMs int     `yaml:"pause_paragraph_ms"`
	PauseChapterMs   int     `yaml:"pause_chapter_ms"`
	LoudnessLUFS     float64 `yaml:"loudness_lufs"`
	OutputFormat     string  `yaml:"output_format"`
	KeepChunks       bool    `yaml:"keep_chunks"`

	// DBPath and OutputDir aren't in the brief's settings.yaml sample but
	// are needed the same way scenario's settings.yaml already carries
	// them — where the SQLite history lives and where per-script output
	// directories get created.
	DBPath    string `yaml:"db_path"`
	OutputDir string `yaml:"output_dir"`

	// ScenarioBundleDir is where scenario's `gen export --format bundle`
	// writes each script's <id>/ directory — the ONLY thing voiceover
	// reads from the scenario module (see internal/manifest). Defaults to
	// the sibling module's output dir, matching go.work's layout; override
	// if scenario exports somewhere else.
	ScenarioBundleDir string `yaml:"scenario_bundle_dir"`
}

// Load reads and parses settings.yaml at path, filling in every field the
// brief documents a default for.
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
	if s.KokoroURL == "" {
		s.KokoroURL = "http://localhost:8880"
	}
	if s.ChunkMaxChars <= 0 {
		s.ChunkMaxChars = 400
	}
	if s.Concurrency <= 0 {
		s.Concurrency = 2
	}
	if s.Speed <= 0 {
		s.Speed = 1.0
	}
	if s.PauseParagraphMs <= 0 {
		s.PauseParagraphMs = 350
	}
	if s.PauseChapterMs <= 0 {
		s.PauseChapterMs = 700
	}
	if s.LoudnessLUFS == 0 {
		s.LoudnessLUFS = -14
	}
	if s.OutputFormat == "" {
		s.OutputFormat = "wav"
	}
	if s.DBPath == "" {
		s.DBPath = "output/voiceover.db"
	}
	if s.OutputDir == "" {
		s.OutputDir = "output"
	}
	if s.ScenarioBundleDir == "" {
		s.ScenarioBundleDir = "../scenario/output/scripts"
	}
}
