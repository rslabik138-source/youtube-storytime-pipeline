package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTiming(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "timing.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write timing.json: %v", err)
	}
	return path
}

func validTimingJSON() string {
	return `{
		"id": "script-1", "voice": "af_aoede", "total_seconds": 120.5,
		"chapters": [{"index": 1, "beat": "hook", "start": 0, "end": 60}],
		"chunks": [
			{"index": 0, "chapter": 1, "start": 0, "end": 14.71, "text": "My name is Clara Vance and I am forty-three years old."}
		]
	}`
}

func TestLoadTimingParsesValidFile(t *testing.T) {
	path := writeTiming(t, t.TempDir(), validTimingJSON())
	tm, err := LoadTiming(path)
	if err != nil {
		t.Fatalf("LoadTiming: %v", err)
	}
	if tm.ID != "script-1" || tm.TotalSeconds != 120.5 {
		t.Fatalf("unexpected timing: %+v", tm)
	}
	if len(tm.Chunks) != 1 || tm.Chunks[0].Text != "My name is Clara Vance and I am forty-three years old." {
		t.Fatalf("unexpected chunks: %+v", tm.Chunks)
	}
}

func TestLoadTimingMissingFileReturnsErrNotFound(t *testing.T) {
	_, err := LoadTiming(filepath.Join(t.TempDir(), "timing.json"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadTimingMalformedJSONReturnsErrInvalid(t *testing.T) {
	path := writeTiming(t, t.TempDir(), `{not valid`)
	_, err := LoadTiming(path)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}

func TestLoadTimingZeroTotalSecondsReturnsErrInvalid(t *testing.T) {
	path := writeTiming(t, t.TempDir(), `{"id":"s1","total_seconds":0,"chunks":[{"text":"x"}]}`)
	_, err := LoadTiming(path)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for zero total_seconds, got %v", err)
	}
}

func TestLoadTimingNoChunksReturnsErrInvalid(t *testing.T) {
	path := writeTiming(t, t.TempDir(), `{"id":"s1","total_seconds":10,"chunks":[]}`)
	_, err := LoadTiming(path)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for no chunks, got %v", err)
	}
}
