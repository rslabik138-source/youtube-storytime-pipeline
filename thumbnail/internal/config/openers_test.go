package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeOpeners(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "thumbnail_openers.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestLoadOpenersHappyPath(t *testing.T) {
	p := writeOpeners(t, "openers:\n  - id: a\n    pattern: \"FOR {years} YEARS I {action}.\"\n  - id: b\n    pattern: \"{amount} GONE.\"\n")
	lib, err := LoadOpeners(p)
	if err != nil {
		t.Fatalf("LoadOpeners: %v", err)
	}
	if len(lib.Openers) != 2 {
		t.Fatalf("expected 2 openers, got %d", len(lib.Openers))
	}
	if o, ok := lib.ByID("b"); !ok || o.Pattern != "{amount} GONE." {
		t.Fatalf("ByID(b) = %+v, %v", o, ok)
	}
}

func TestLoadOpenersRejectsEmpty(t *testing.T) {
	p := writeOpeners(t, "openers: []\n")
	if _, err := LoadOpeners(p); err == nil {
		t.Fatalf("expected an error for a library with no openers")
	}
}

func TestLoadOpenersRejectsMissingFields(t *testing.T) {
	p := writeOpeners(t, "openers:\n  - id: a\n")
	if _, err := LoadOpeners(p); err == nil {
		t.Fatalf("expected an error for an opener with no pattern")
	}
}

func TestLoadOpenersRejectsDuplicateID(t *testing.T) {
	p := writeOpeners(t, "openers:\n  - id: a\n    pattern: X\n  - id: a\n    pattern: Y\n")
	if _, err := LoadOpeners(p); err == nil {
		t.Fatalf("expected an error for a duplicate opener id")
	}
}

func TestLoadRealOpenersConfig(t *testing.T) {
	// The checked-in config must always load and offer enough openers to
	// rotate without immediately repeating within the avoid window.
	lib, err := LoadOpeners("../../configs/thumbnail_openers.yaml")
	if err != nil {
		t.Fatalf("LoadOpeners(real): %v", err)
	}
	if len(lib.Openers) < 4 {
		t.Fatalf("expected at least 4 openers to rotate over, got %d", len(lib.Openers))
	}
}
