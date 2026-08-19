// Package manifest reads the ONLY thing voiceover is allowed to know about
// a scenario script: the exported bundle directory scenario's `gen export
// --format bundle` writes (script.txt + manifest.json). No SQLite, no
// direct dependency on the scenario module — the file pair is the entire
// contract between the two modules.
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

// Chapter mirrors scenario's export.BundleChapter. CharStart/CharEnd are
// byte offsets into the sibling script.txt, not rune offsets — scenario's
// Bundle() computes them the same way, over the raw UTF-8 bytes.
type Chapter struct {
	Index     int    `json:"index"`
	Beat      string `json:"beat"`
	Words     int    `json:"words"`
	CharStart int    `json:"char_start"`
	CharEnd   int    `json:"char_end"`
}

// Manifest mirrors scenario's export.BundleManifest — manifest.json's
// exact shape. Changing this struct's JSON tags is a cross-module contract
// change, not a local refactor; it must stay in lockstep with scenario's
// internal/export.BundleManifest.
type Manifest struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	WordCount int       `json:"word_count"`
	Narrator  Narrator  `json:"narrator"`
	Chapters  []Chapter `json:"chapters"`
}

// Bundle is a loaded scenario export: the manifest plus the exact script
// text it indexes into. Every downstream package (chunk, catalog) works
// from this, never from the filesystem directly.
type Bundle struct {
	Dir      string
	Manifest Manifest
	Text     string
}

var (
	// ErrNotFound means script.txt or manifest.json is missing from dir —
	// scenario's bundle export was never run for this ID, or dir points
	// somewhere else. Distinguishable from ErrInvalid via errors.Is so the
	// CLI can print "did you run `gen export --format bundle`?" instead of
	// a generic parse error.
	ErrNotFound = errors.New("manifest: script bundle not found")
	// ErrInvalid means manifest.json exists but doesn't parse as JSON, or
	// fails a cross-check against script.txt (a chapter's char range
	// outside the text, a missing required field).
	ErrInvalid = errors.New("manifest: invalid manifest.json")
)

// Load reads dir/script.txt and dir/manifest.json and validates the
// manifest against the text. Never panics on bad input: a missing file
// wraps ErrNotFound, a malformed or inconsistent manifest wraps ErrInvalid.
func Load(dir string) (*Bundle, error) {
	textPath := filepath.Join(dir, "script.txt")
	textBytes, err := os.ReadFile(textPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", textPath, ErrNotFound)
		}
		return nil, fmt.Errorf("read %s: %w", textPath, err)
	}

	manifestPath := filepath.Join(dir, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", manifestPath, ErrNotFound)
		}
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}

	var m Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w: %w", manifestPath, ErrInvalid, err)
	}

	text := string(textBytes)
	if err := validate(m, text); err != nil {
		return nil, fmt.Errorf("%s: %w: %w", manifestPath, ErrInvalid, err)
	}

	return &Bundle{Dir: dir, Manifest: m, Text: text}, nil
}

func validate(m Manifest, text string) error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("missing id")
	}
	if m.WordCount <= 0 {
		return fmt.Errorf("word_count must be > 0, got %d", m.WordCount)
	}
	if strings.TrimSpace(m.Narrator.Name) == "" {
		return errors.New("missing narrator.name")
	}
	if len(m.Chapters) == 0 {
		return errors.New("no chapters")
	}
	for _, c := range m.Chapters {
		if c.CharStart < 0 || c.CharEnd > len(text) || c.CharStart > c.CharEnd {
			return fmt.Errorf("chapter %d has an invalid char range [%d,%d) for a %d-byte script.txt",
				c.Index, c.CharStart, c.CharEnd, len(text))
		}
	}
	return nil
}
