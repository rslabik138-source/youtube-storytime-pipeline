// Package openerpicker chooses which opening-line construction a thumbnail
// uses, rotating deterministically across `thumb generate` runs so the
// channel's thumbnails stop all opening with the same template. It mirrors
// internal/facepicker: a pure Pick over the library plus a JSON History
// that carries "recently used" across invocations.
package openerpicker

import (
	"fmt"

	"github.com/placeholder/thumbnail/internal/config"
)

// Pick returns the next opener to use given recent (opener IDs used most
// recently, most-recent-first). It is deterministic: it walks the library
// in file order and returns the first opener not among the last
// avoidRecent used. If every opener is in that recent window (avoidRecent
// >= library size), it falls back to the one used longest ago — never
// erroring, so a small library or a long history can't wedge generation.
func Pick(lib config.OpenerLibrary, recent []string, avoidRecent int) (config.Opener, error) {
	if len(lib.Openers) == 0 {
		return config.Opener{}, fmt.Errorf("openerpicker: empty opener library")
	}

	avoid := map[string]bool{}
	for i, id := range recent {
		if avoidRecent > 0 && i >= avoidRecent {
			break
		}
		avoid[id] = true
	}

	for _, o := range lib.Openers {
		if !avoid[o.ID] {
			return o, nil
		}
	}

	// Every opener is within the avoid window — pick the one used longest
	// ago (the last entry of recent that's actually in the library), or the
	// first opener if recent names something no longer in the library.
	for i := len(recent) - 1; i >= 0; i-- {
		if o, ok := lib.ByID(recent[i]); ok {
			return o, nil
		}
	}
	return lib.Openers[0], nil
}
