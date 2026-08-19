package openerpicker

import (
	"testing"

	"github.com/placeholder/thumbnail/internal/config"
)

func lib() config.OpenerLibrary {
	return config.OpenerLibrary{Openers: []config.Opener{
		{ID: "a", Pattern: "A {x}"},
		{ID: "b", Pattern: "B {x}"},
		{ID: "c", Pattern: "C {x}"},
		{ID: "d", Pattern: "D {x}"},
	}}
}

func TestPickFirstWhenNoHistory(t *testing.T) {
	o, err := Pick(lib(), nil, 3)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if o.ID != "a" {
		t.Fatalf("expected the first opener with no history, got %q", o.ID)
	}
}

func TestPickSkipsRecent(t *testing.T) {
	// recent = a, b (most recent first) with avoid window 3 -> skip a,b -> c.
	o, err := Pick(lib(), []string{"a", "b"}, 3)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if o.ID != "c" {
		t.Fatalf("expected to skip recent a,b and pick c, got %q", o.ID)
	}
}

func TestPickHonorsAvoidWindow(t *testing.T) {
	// avoidRecent=1 means only the single most-recent is off-limits, so with
	// recent = b, a the picker may return a (it's outside the 1-item window
	// once b is excluded) — the first library opener not equal to b is a.
	o, err := Pick(lib(), []string{"b", "a"}, 1)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if o.ID != "a" {
		t.Fatalf("expected a (only b is within the 1-item avoid window), got %q", o.ID)
	}
}

func TestPickFallsBackWhenAllRecent(t *testing.T) {
	// Every opener is within the avoid window: fall back to the one used
	// longest ago (last entry of recent), never error.
	o, err := Pick(lib(), []string{"a", "b", "c", "d"}, 4)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if o.ID != "d" {
		t.Fatalf("expected fallback to the longest-ago opener d, got %q", o.ID)
	}
}

func TestPickEmptyLibraryErrors(t *testing.T) {
	if _, err := Pick(config.OpenerLibrary{}, nil, 3); err == nil {
		t.Fatalf("expected an error for an empty opener library")
	}
}

func TestHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	h := History{Path: dir + "/opener_history.json"}

	if last, err := h.Last(5); err != nil || len(last) != 0 {
		t.Fatalf("expected empty history on first read, got %v (err %v)", last, err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if err := h.Record(id, 10); err != nil {
			t.Fatalf("Record %q: %v", id, err)
		}
	}
	last, err := h.Last(2)
	if err != nil {
		t.Fatalf("Last: %v", err)
	}
	if len(last) != 2 || last[0] != "c" || last[1] != "b" {
		t.Fatalf("expected most-recent-first [c b], got %v", last)
	}
}
