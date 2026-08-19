// Package store persists a local history of completed voiceovers — which
// voice was used for which script, when, how long, how big — entirely
// separate from scenario's own SQLite database. voiceover never opens
// scenario's DB; the only thing it reads from scenario is the exported
// bundle directory (see internal/manifest).
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by GetVoiceover when scriptID has no recorded
// voiceover.
var ErrNotFound = errors.New("store: voiceover not found")

// VoiceoverSummary is one completed voiceover run.
type VoiceoverSummary struct {
	ScriptID     string
	Voice        string
	CreatedAt    time.Time
	TotalSeconds float64
	SizeBytes    int64
}

// StatsSummary aggregates across every recorded voiceover, for `voice stats`.
type StatsSummary struct {
	TotalVoiceovers int
	TotalSeconds    float64
	TotalSizeBytes  int64
}

// Store is everything catalog.Select and cmd/voice need from persistence.
// Two implementations satisfy it: sqliteStore (modernc.org/sqlite, the
// real backend) and MemoryStore (in-process, for tests) — both exercised
// by the same conformance suite in store_test.go.
type Store interface {
	// RecentUsedVoices returns the last n recorded voiceovers' voice IDs,
	// most-recent-first — catalog.Select's "don't repeat the last 3" rule.
	RecentUsedVoices(ctx context.Context, n int) ([]string, error)
	// RecordVoiceover upserts one script's voiceover record — call once,
	// right after Assemble succeeds. A second call for the same ScriptID
	// (e.g. re-voicing after a bad take) replaces the previous record
	// rather than accumulating a duplicate.
	RecordVoiceover(ctx context.Context, v VoiceoverSummary) error
	// GetVoiceover returns ErrNotFound if scriptID has no recorded voiceover.
	GetVoiceover(ctx context.Context, scriptID string) (VoiceoverSummary, error)
	ListVoiceovers(ctx context.Context) ([]VoiceoverSummary, error)
	Stats(ctx context.Context) (StatsSummary, error)
	Close() error
}
