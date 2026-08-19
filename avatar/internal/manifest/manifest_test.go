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
		"narrator": {
			"name": "Clara Vance", "age": 43, "sex": "female",
			"build": "average", "hair": "gray, cropped short",
			"face_note": "reading glasses pushed up into the hair",
			"attire": "a plain button-down with the sleeves rolled"
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
		t.Fatalf("unexpected narrator core fields: %+v", m.Narrator)
	}
	if m.Narrator.Build != "average" || m.Narrator.Hair != "gray, cropped short" ||
		m.Narrator.FaceNote != "reading glasses pushed up into the hair" ||
		m.Narrator.Attire != "a plain button-down with the sleeves rolled" {
		t.Fatalf("unexpected narrator appearance fields: %+v", m.Narrator)
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

func TestLoadMissingProfessionReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"id": "script-1", "title": "T", "profession": "", "word_count": 100,
		"narrator": {"name": "Clara Vance", "age": 43, "sex": "female"}
	}`)
	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a missing profession, got %v", err)
	}
}

func TestLoadMissingNarratorSexReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"id": "script-1", "title": "T", "profession": "nurse", "word_count": 100,
		"narrator": {"name": "Clara Vance", "age": 43, "sex": ""}
	}`)
	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a missing narrator.sex, got %v", err)
	}
}

func TestLoadZeroAgeReturnsErrInvalid(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"id": "script-1", "title": "T", "profession": "nurse", "word_count": 100,
		"narrator": {"name": "Clara Vance", "age": 0, "sex": "female"}
	}`)
	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for age 0, got %v", err)
	}
}

// TestLoadOldBundleWithoutAppearanceFieldsStillLoads mirrors a real
// observed case: a script exported before the appearance fields
// (build/hair/face_note/attire) existed. Those are optional — the
// manifest is still usable, just with blanks the prompt template has to
// handle gracefully (see prompts/avatar.tmpl).
func TestLoadOldBundleWithoutAppearanceFieldsStillLoads(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"id": "script-1", "title": "T", "profession": "accountant", "word_count": 7336,
		"narrator": {"name": "Clara Vance", "age": 43, "sex": "female"}
	}`)
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v (old bundles without appearance fields must still load)", err)
	}
	if m.Narrator.Build != "" || m.Narrator.Hair != "" || m.Narrator.FaceNote != "" || m.Narrator.Attire != "" {
		t.Fatalf("expected blank appearance fields to round-trip as empty, got %+v", m.Narrator)
	}
}
