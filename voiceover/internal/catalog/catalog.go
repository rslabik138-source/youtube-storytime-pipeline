// Package catalog manages the hand-curated voice catalog
// (configs/voices.yaml) and picks a voice for a given narrator.
package catalog

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Voice is one configs/voices.yaml entry, filled in by hand after
// listening to a Kokoro voice sample — nothing here is inferred
// automatically from Kokoro itself.
type Voice struct {
	ID      string `yaml:"id"`
	Sex     string `yaml:"sex"`
	Accent  string `yaml:"accent"`
	AgeFeel [2]int `yaml:"age_feel"`
	Texture string `yaml:"texture"`
}

// Catalog is the parsed configs/voices.yaml.
type Catalog struct {
	Voices []Voice `yaml:"voices"`
}

// Load reads and parses a voices.yaml file at path.
func Load(path string) (Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, fmt.Errorf("catalog: read %s: %w", path, err)
	}
	var c Catalog
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Catalog{}, fmt.Errorf("catalog: parse %s: %w", path, err)
	}
	return c, nil
}

// IDs returns every voice ID in the catalog, in file order — used to build
// a "here's what's available" message when Select can't find a match.
func (c Catalog) IDs() []string {
	out := make([]string, len(c.Voices))
	for i, v := range c.Voices {
		out[i] = v.ID
	}
	return out
}
