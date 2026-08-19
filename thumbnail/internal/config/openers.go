package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Opener is one opening-line construction from thumbnail_openers.yaml — a
// fixed SHAPE for a thumbnail's first line (e.g. `FOR {years} YEARS I
// {action}.`), whose {placeholders} the text-generation model fills from
// the story facts. thumb rotates through these so thumbnails stop all
// opening with the same "FOR X YEARS I..." template.
type Opener struct {
	ID      string `yaml:"id"`
	Pattern string `yaml:"pattern"`
}

// OpenerLibrary is thumbnail_openers.yaml's shape.
type OpenerLibrary struct {
	Openers []Opener `yaml:"openers"`
}

// ByID returns the opener with the given id, or false if there is none.
func (l OpenerLibrary) ByID(id string) (Opener, bool) {
	for _, o := range l.Openers {
		if o.ID == id {
			return o, true
		}
	}
	return Opener{}, false
}

// LoadOpeners reads and parses thumbnail_openers.yaml at path. A library
// with no openers, or an opener missing its id or pattern, is an error —
// the picker can't rotate over nothing, and a blank pattern would silently
// impose no constraint on the opening line.
func LoadOpeners(path string) (OpenerLibrary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return OpenerLibrary{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	var l OpenerLibrary
	if err := yaml.Unmarshal(data, &l); err != nil {
		return OpenerLibrary{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if len(l.Openers) == 0 {
		return OpenerLibrary{}, fmt.Errorf("config: %s defines no openers", path)
	}
	seen := map[string]bool{}
	for i, o := range l.Openers {
		if strings.TrimSpace(o.ID) == "" {
			return OpenerLibrary{}, fmt.Errorf("config: %s opener %d has no id", path, i+1)
		}
		if strings.TrimSpace(o.Pattern) == "" {
			return OpenerLibrary{}, fmt.Errorf("config: %s opener %q has no pattern", path, o.ID)
		}
		if seen[o.ID] {
			return OpenerLibrary{}, fmt.Errorf("config: %s has duplicate opener id %q", path, o.ID)
		}
		seen[o.ID] = true
	}
	return l, nil
}
