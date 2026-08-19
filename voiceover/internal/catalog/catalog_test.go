package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesVoicesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "voices.yaml")
	content := `
voices:
  - id: af_bella
    sex: female
    accent: us
    age_feel: [30, 45]
    texture: warm
  - id: am_adam
    sex: male
    accent: us
    age_feel: [25, 50]
    texture: neutral
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write voices.yaml: %v", err)
	}

	cat, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cat.Voices) != 2 {
		t.Fatalf("expected 2 voices, got %d", len(cat.Voices))
	}
	if cat.Voices[0].ID != "af_bella" || cat.Voices[0].Sex != "female" || cat.Voices[0].AgeFeel != [2]int{30, 45} {
		t.Fatalf("unexpected first voice: %+v", cat.Voices[0])
	}
	if got := cat.IDs(); len(got) != 2 || got[0] != "af_bella" || got[1] != "am_adam" {
		t.Fatalf("unexpected IDs(): %v", got)
	}
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}

func TestLoadMalformedYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "voices.yaml")
	if err := os.WriteFile(path, []byte("voices: [this is not valid: yaml: at all"), 0o644); err != nil {
		t.Fatalf("write voices.yaml: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatalf("expected an error for malformed YAML")
	}
}
