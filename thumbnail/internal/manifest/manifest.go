// Package manifest reads the ONLY thing thumbnail is allowed to know about
// a scenario script: the exported bundle directory scenario's `gen export
// --format bundle` writes (script.txt + manifest.json). No SQLite, no
// direct dependency on the scenario module — the file pair is the entire
// contract between the two modules. thumbnail only ever reads
// manifest.json; script.txt exists for voiceover's sake, not this
// module's.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Narrator mirrors scenario's export.BundleNarrator JSON shape — kept as
// an independent struct (not an import of the scenario module) since the
// contract between the two modules is the file pair, not a shared Go type.
type Narrator struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
	Sex  string `json:"sex"`
}

// Story mirrors scenario's export.BundleStory — the seed/bible facts
// thumbnail's text generation needs (see internal/textgen), deliberately
// just the abstract facts rather than full chapter prose.
type Story struct {
	FamilyLaw     string            `json:"family_law"`
	RefrainPhrase string            `json:"refrain_phrase"`
	SeededLine    string            `json:"seeded_line"`
	Antagonist    string            `json:"antagonist"`
	Betrayal      string            `json:"betrayal"`
	EndingType    string            `json:"ending_type"`
	Numbers       map[string]string `json:"numbers"`
}

// Manifest mirrors scenario's export.BundleManifest. Changing this
// struct's JSON tags is a cross-module contract change, not a local
// refactor; it must stay in lockstep with scenario's
// internal/export.BundleManifest. thumbnail doesn't need Chapters at all
// (that's script.txt/voiceover's concern) or the narrator's appearance
// fields (that's avatar's concern), so both are omitted here entirely —
// json.Unmarshal ignores fields with no matching struct field.
type Manifest struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Profession string   `json:"profession"`
	WordCount  int      `json:"word_count"`
	Narrator   Narrator `json:"narrator"`
	Story      Story    `json:"story"`
}

var (
	// ErrNotFound means manifest.json is missing from dir — scenario's
	// bundle export was never run for this ID, or dir points somewhere
	// else.
	ErrNotFound = errors.New("manifest: script bundle not found")
	// ErrInvalid means manifest.json exists but doesn't parse as JSON, or
	// fails a cross-check (a missing required field for building thumbnail
	// text).
	ErrInvalid = errors.New("manifest: invalid manifest.json")
)

// Load reads dir/manifest.json and validates it. Never panics on bad
// input: a missing file wraps ErrNotFound, a malformed or incomplete
// manifest wraps ErrInvalid.
func Load(dir string) (*Manifest, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", manifestPath, ErrNotFound)
		}
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w: %w", manifestPath, ErrInvalid, err)
	}
	if err := validate(m); err != nil {
		return nil, fmt.Errorf("%s: %w: %w", manifestPath, ErrInvalid, err)
	}
	return &m, nil
}

func validate(m Manifest) error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("missing id")
	}
	if strings.TrimSpace(m.Narrator.Name) == "" {
		return errors.New("missing narrator.name")
	}
	if strings.TrimSpace(m.Narrator.Sex) == "" {
		return errors.New("missing narrator.sex")
	}
	// Story facts (family_law, refrain_phrase, etc) are the whole point of
	// thumbnail text generation — a manifest with none of them can't
	// produce a meaningful prompt. RefrainPhrase alone is required as the
	// floor; the others are used opportunistically when present.
	if strings.TrimSpace(m.Story.RefrainPhrase) == "" {
		return errors.New("missing story.refrain_phrase")
	}
	return nil
}
