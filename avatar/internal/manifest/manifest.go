// Package manifest reads the ONLY thing avatar is allowed to know about a
// scenario script: the exported bundle directory scenario's `gen export
// --format bundle` writes (script.txt + manifest.json). No SQLite, no
// direct dependency on the scenario module — the file pair is the entire
// contract between the two modules. avatar only ever reads manifest.json;
// script.txt exists for voiceover's sake, not this module's.
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
	Name     string `json:"name"`
	Age      int    `json:"age"`
	Sex      string `json:"sex"`
	Build    string `json:"build"`
	Hair     string `json:"hair"`
	FaceNote string `json:"face_note"`
	Attire   string `json:"attire"`
}

// Manifest mirrors scenario's export.BundleManifest. Changing this
// struct's JSON tags is a cross-module contract change, not a local
// refactor; it must stay in lockstep with scenario's
// internal/export.BundleManifest. avatar doesn't need Chapters at all
// (that's script.txt/voiceover's concern), so it's omitted here entirely —
// json.Unmarshal ignores fields with no matching struct field.
type Manifest struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Profession string   `json:"profession"`
	WordCount  int      `json:"word_count"`
	Narrator   Narrator `json:"narrator"`
}

var (
	// ErrNotFound means manifest.json is missing from dir — scenario's
	// bundle export was never run for this ID, or dir points somewhere
	// else.
	ErrNotFound = errors.New("manifest: script bundle not found")
	// ErrInvalid means manifest.json exists but doesn't parse as JSON, or
	// fails a cross-check (a missing required field for building a
	// portrait prompt).
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
	if strings.TrimSpace(m.Profession) == "" {
		return errors.New("missing profession")
	}
	if strings.TrimSpace(m.Narrator.Name) == "" {
		return errors.New("missing narrator.name")
	}
	if strings.TrimSpace(m.Narrator.Sex) == "" {
		return errors.New("missing narrator.sex")
	}
	if m.Narrator.Age <= 0 {
		return fmt.Errorf("narrator.age must be > 0, got %d", m.Narrator.Age)
	}
	return nil
}
