package openerpicker

import (
	"encoding/json"
	"fmt"
	"os"
)

// History persists which opener IDs were used, most-recently-used first, so
// Pick's rotation survives across separate `thumb generate` invocations —
// the same role facepicker.History plays for faces. Kept as its own type
// (rather than importing facepicker's) so the two histories stay
// independent files with independent semantics.
type History struct {
	Path string
}

type historyFile struct {
	Used []string `json:"used"` // most recent first
}

// Last returns up to n of the most recently used opener IDs, most recent
// first. A missing history file (first run ever) is not an error — nothing
// has been used yet.
func (h History) Last(n int) ([]string, error) {
	data, err := os.ReadFile(h.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("openerpicker: read %s: %w", h.Path, err)
	}
	var f historyFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("openerpicker: parse %s: %w", h.Path, err)
	}
	if n > 0 && len(f.Used) > n {
		return f.Used[:n], nil
	}
	return f.Used, nil
}

// Record prepends id to the history (most recent first) and persists it,
// keeping only the most recent keepMax entries.
func (h History) Record(id string, keepMax int) error {
	f := historyFile{}
	if data, err := os.ReadFile(h.Path); err == nil {
		if uerr := json.Unmarshal(data, &f); uerr != nil {
			return fmt.Errorf("openerpicker: parse %s: %w", h.Path, uerr)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("openerpicker: read %s: %w", h.Path, err)
	}

	f.Used = append([]string{id}, f.Used...)
	if keepMax > 0 && len(f.Used) > keepMax {
		f.Used = f.Used[:keepMax]
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("openerpicker: marshal history: %w", err)
	}
	if err := os.WriteFile(h.Path, data, 0o644); err != nil {
		return fmt.Errorf("openerpicker: write %s: %w", h.Path, err)
	}
	return nil
}
