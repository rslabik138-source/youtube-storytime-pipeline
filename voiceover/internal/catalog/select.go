package catalog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/placeholder/voiceover/internal/manifest"
)

// ErrNoCandidates means no catalog voice matches narrator even after
// relaxing the age_feel filter — wrapped with every catalog voice ID (via
// fmt.Errorf's %w plus the list in the message) so the caller can print a
// clear, actionable error instead of a bare "not found".
var ErrNoCandidates = errors.New("catalog: no voice matches the narrator")

// Select picks a voice for narrator, preferring one not used in any of the
// last few accepted voiceovers (usedRecently, from store.RecentUsedVoices).
// Filter order:
//  1. sex must match exactly.
//  2. narrator.age must fall within age_feel[0..1] inclusive.
//  3. exclude usedRecently.
//  4. if step 2+3 together leave no candidate, drop the age_feel filter
//     and retry sex-only + not-recently-used.
//  5. if that's still empty, ErrNoCandidates, listing every catalog voice
//     ID.
//
// --voice on the CLI bypasses this entirely; Select is only the automatic
// path.
func Select(cat Catalog, narrator manifest.Narrator, usedRecently []string) (Voice, error) {
	used := make(map[string]bool, len(usedRecently))
	for _, id := range usedRecently {
		used[id] = true
	}

	bySex := filterBySex(cat.Voices, narrator.Sex)
	byAge := filterByAge(bySex, narrator.Age)

	if v, ok := firstNotUsed(byAge, used); ok {
		return v, nil
	}
	if v, ok := firstNotUsed(bySex, used); ok {
		return v, nil
	}

	return Voice{}, fmt.Errorf("%w (sex=%q age=%d) — available voices: %s",
		ErrNoCandidates, narrator.Sex, narrator.Age, strings.Join(cat.IDs(), ", "))
}

func filterBySex(voices []Voice, sex string) []Voice {
	var out []Voice
	for _, v := range voices {
		if strings.EqualFold(v.Sex, sex) {
			out = append(out, v)
		}
	}
	return out
}

func filterByAge(voices []Voice, age int) []Voice {
	var out []Voice
	for _, v := range voices {
		if age >= v.AgeFeel[0] && age <= v.AgeFeel[1] {
			out = append(out, v)
		}
	}
	return out
}

func firstNotUsed(voices []Voice, used map[string]bool) (Voice, bool) {
	for _, v := range voices {
		if !used[v.ID] {
			return v, true
		}
	}
	return Voice{}, false
}
