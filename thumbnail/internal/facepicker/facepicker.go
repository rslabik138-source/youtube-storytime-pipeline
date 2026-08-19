// Package facepicker selects a portrait from the channel's standing face
// library (configs/faces.yaml) for a given narrator — matching sex and age,
// and rotating so the same face doesn't appear on two thumbnails in a row
// (or three: the last 2 used are excluded, not just the last 1).
package facepicker

import (
	"fmt"

	"github.com/placeholder/thumbnail/internal/config"
)

// Pick returns a face from lib matching sex and age, excluding whichever
// face IDs appear in recentlyUsed (most recent first; only the first 2 are
// consulted — see History.Last). Candidates are considered in lib's own
// order, so with recentlyUsed correctly fed back call to call, repeated
// calls naturally round-robin through every eligible face instead of
// favoring one.
//
// If excluding recentlyUsed would leave no eligible face (e.g. only 1-2
// faces match this sex/age bracket at all), the exclusion is dropped and
// picks from every matching face instead — rotation is a nice-to-have, a
// thumbnail existing at all is not optional.
func Pick(lib config.FaceLibrary, sex string, age int, recentlyUsed []string) (config.Face, error) {
	var candidates []config.Face
	for _, f := range lib.Faces {
		if f.Matches(sex, age) {
			candidates = append(candidates, f)
		}
	}
	if len(candidates) == 0 {
		return config.Face{}, fmt.Errorf("facepicker: no face in the library matches sex=%q age=%d", sex, age)
	}

	excluded := map[string]bool{}
	for i, id := range recentlyUsed {
		if i >= 2 {
			break
		}
		excluded[id] = true
	}

	for _, f := range candidates {
		if !excluded[f.ID] {
			return f, nil
		}
	}
	// Every candidate was recently used — fall back to the full candidate
	// set rather than failing.
	return candidates[0], nil
}
