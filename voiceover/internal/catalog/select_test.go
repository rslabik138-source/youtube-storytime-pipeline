package catalog

import (
	"errors"
	"strings"
	"testing"

	"github.com/placeholder/voiceover/internal/manifest"
)

func testCatalog() Catalog {
	return Catalog{Voices: []Voice{
		{ID: "af_bella", Sex: "female", AgeFeel: [2]int{30, 45}, Texture: "warm"},
		{ID: "af_older", Sex: "female", AgeFeel: [2]int{50, 65}, Texture: "measured"},
		{ID: "am_adam", Sex: "male", AgeFeel: [2]int{25, 50}, Texture: "neutral"},
	}}
}

func TestSelectFiltersBySexAndAge(t *testing.T) {
	v, err := Select(testCatalog(), manifest.Narrator{Sex: "female", Age: 43}, nil)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if v.ID != "af_bella" {
		t.Fatalf("expected af_bella (sex+age match), got %q", v.ID)
	}
}

func TestSelectExcludesRecentlyUsedVoices(t *testing.T) {
	// Both female voices exist, but af_bella (the only one whose age_feel
	// covers 43) was just used — sex+age match alone would still return
	// af_bella if the "used recently" rule weren't respected... but only
	// af_bella matches age 43 here, so this test uses an age that both
	// female voices could plausibly cover isn't realistic; instead verify
	// the exclusion directly: with af_bella used, age-matching is dropped
	// and the next best sex-only match (af_older, not recently used) wins.
	v, err := Select(testCatalog(), manifest.Narrator{Sex: "female", Age: 43}, []string{"af_bella"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if v.ID == "af_bella" {
		t.Fatalf("expected af_bella to be excluded as recently used")
	}
	if v.ID != "af_older" {
		t.Fatalf("expected the sex-only fallback (af_older), got %q", v.ID)
	}
}

func TestSelectRelaxesAgeFeelWhenNoAgeMatch(t *testing.T) {
	// Age 80 matches no female voice's age_feel — falls back to sex-only.
	v, err := Select(testCatalog(), manifest.Narrator{Sex: "female", Age: 80}, nil)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if v.Sex != "female" {
		t.Fatalf("expected a female voice from the sex-only fallback, got %+v", v)
	}
}

func TestSelectNoCandidatesReturnsErrNoCandidatesWithVoiceList(t *testing.T) {
	_, err := Select(testCatalog(), manifest.Narrator{Sex: "nonbinary", Age: 40}, nil)
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("expected ErrNoCandidates, got %v", err)
	}
	for _, id := range []string{"af_bella", "af_older", "am_adam"} {
		if !strings.Contains(err.Error(), id) {
			t.Fatalf("expected the error to list voice %q, got: %v", id, err)
		}
	}
}

func TestSelectEmptyCatalogReturnsErrNoCandidates(t *testing.T) {
	_, err := Select(Catalog{}, manifest.Narrator{Sex: "female", Age: 40}, nil)
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("expected ErrNoCandidates for an empty catalog, got %v", err)
	}
}
