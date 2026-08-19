package facepicker

import (
	"testing"

	"github.com/placeholder/thumbnail/internal/config"
)

func testLibrary() config.FaceLibrary {
	return config.FaceLibrary{Faces: []config.Face{
		{ID: "face-01", Sex: "female", AgeFeel: [2]int{35, 50}},
		{ID: "face-02", Sex: "female", AgeFeel: [2]int{35, 50}},
		{ID: "face-03", Sex: "female", AgeFeel: [2]int{35, 50}},
		{ID: "face-04", Sex: "male", AgeFeel: [2]int{40, 60}},
	}}
}

func TestPickFiltersBySexAndAge(t *testing.T) {
	f, err := Pick(testLibrary(), "male", 45, nil)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if f.ID != "face-04" {
		t.Fatalf("expected face-04 (the only male match), got %q", f.ID)
	}
}

func TestPickNoMatchReturnsError(t *testing.T) {
	if _, err := Pick(testLibrary(), "male", 20, nil); err == nil {
		t.Fatalf("expected an error when no face matches sex/age")
	}
}

func TestPickExcludesLastTwoUsed(t *testing.T) {
	f, err := Pick(testLibrary(), "female", 40, []string{"face-01", "face-02"})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if f.ID != "face-03" {
		t.Fatalf("expected face-03 (the only one not in the last 2 used), got %q", f.ID)
	}
}

func TestPickOnlyConsidersTheLastTwoOfRecentlyUsed(t *testing.T) {
	// face-03 was used 3 calls ago — old enough that it's eligible again.
	f, err := Pick(testLibrary(), "female", 40, []string{"face-01", "face-02", "face-03"})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if f.ID != "face-03" {
		t.Fatalf("expected face-03 (3rd-most-recent, past the last-2 window), got %q", f.ID)
	}
}

func TestPickFallsBackToFullCandidateSetWhenAllRecentlyUsed(t *testing.T) {
	// Only face-04 matches male/45-60; it's also the most recently used —
	// excluding it would leave zero candidates, so it must still be picked.
	f, err := Pick(testLibrary(), "male", 45, []string{"face-04"})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if f.ID != "face-04" {
		t.Fatalf("expected the fallback to still return the only match, got %q", f.ID)
	}
}

func TestPickRoundRobinsThroughEligibleFacesAcrossCalls(t *testing.T) {
	lib := testLibrary()
	var recent []string
	seen := map[string]int{}
	for i := 0; i < 6; i++ {
		f, err := Pick(lib, "female", 40, recent)
		if err != nil {
			t.Fatalf("Pick call %d: %v", i, err)
		}
		seen[f.ID]++
		recent = append([]string{f.ID}, recent...)
	}
	// 3 eligible faces (face-01/02/03), 6 picks — round-robin means each
	// used exactly twice, never the same face twice in a row.
	for _, id := range []string{"face-01", "face-02", "face-03"} {
		if seen[id] != 2 {
			t.Fatalf("expected %s picked exactly twice across 6 round-robin calls, got %d (seen=%+v)", id, seen[id], seen)
		}
	}
}
