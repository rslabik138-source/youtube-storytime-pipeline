package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest.json: %v", err)
	}
}

func validManifestJSON() string {
	return `{
		"id": "script-1",
		"title": "The Ledger",
		"profession": "accountant",
		"word_count": 7336,
		"narrator": {"name": "Clara Vance", "age": 43, "sex": "female"},
		"story": {
			"family_law": "the numbers always add up eventually",
			"refrain_phrase": "I kept the ledger the way I kept everything",
			"seeded_line": "a receipt never lies",
			"antagonist": "mother_in_law",
			"betrayal": "savings_taken",
			"ending_type": "cold_silence",
			"numbers": {"amount": "twelve hundred dollars"}
		}
	}`
}

func TestLoadValidManifest(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, validManifestJSON())

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.ID != "script-1" || m.Profession != "accountant" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if m.Narrator.Name != "Clara Vance" || m.Narrator.Sex != "female" || m.Narrator.Age != 43 {
		t.Fatalf("unexpected narrator fields: %+v", m.Narrator)
	}
	if m.Story.FamilyLaw != "the numbers always add up eventually" ||
		m.Story.RefrainPhrase != "I kept the ledger the way I kept everything" ||
		m.Story.SeededLine != "a receipt never lies" ||
		m.Story.Antagonist != "mother_in_law" || m.Story.Betrayal != "savings_taken" ||
		m.Story.EndingType != "cold_silence" {
		t.Fatalf("unexpected story fields: %+v", m.Story)
	}
	if m.Story.Numbers["amount"] != "twelve hundred dollars" {
		t.Fatalf("expected numbers to round-trip, got %+v", m.Story.Numbers)
	}
}

func TestLoadMissingManifestReturnsErrNotFound(t *testing.T) {
	_, err := Load(t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadMissingDirReturnsErrNotFound(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadMalformedJSONReturnsErrInvalidNotErrNotFound(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{not valid json`)

	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed JSON should be ErrInvalid, not ErrNotFound, got %v", err)
	}
}

func TestLoadMissingNarratorNameReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"id": "script-1", "narrator": {"name": "", "sex": "female"},
		"story": {"refrain_phrase": "r"}
	}`)
	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a missing narrator.name, got %v", err)
	}
}

func TestLoadMissingNarratorSexReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"id": "script-1", "narrator": {"name": "Clara Vance", "sex": ""},
		"story": {"refrain_phrase": "r"}
	}`)
	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a missing narrator.sex, got %v", err)
	}
}

func TestLoadMissingRefrainPhraseReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"id": "script-1", "narrator": {"name": "Clara Vance", "sex": "female"},
		"story": {"refrain_phrase": ""}
	}`)
	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a missing story.refrain_phrase, got %v", err)
	}
}

// TestLoadOldBundleWithoutStoryFieldsReturnsErrInvalid documents, on
// purpose, that thumbnail's requirement is stricter than avatar's: a
// script bundle exported before the Story block existed (all of scenario's
// pre-attire-era scripts) has no refrain_phrase and can't produce
// meaningful thumbnail text — re-export from a current scenario build
// fixes this, since RefrainPhrase always existed on story.Bible itself.
func TestLoadOldBundleWithoutStoryFieldsReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"id": "script-1", "title": "T", "profession": "accountant", "word_count": 7336,
		"narrator": {"name": "Clara Vance", "age": 43, "sex": "female"}
	}`)
	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a manifest with no story block, got %v", err)
	}
}
