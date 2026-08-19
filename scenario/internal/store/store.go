// Package store persists seeds, bibles, chapters, quality scores, and runs,
// and answers the dedup queries internal/seed and internal/generate need to
// avoid repeating professions, axis combinations, character names, places,
// and aphorisms across scripts.
//
// Two implementations satisfy Store: sqliteStore (modernc.org/sqlite, no
// cgo, the real backend) and MemoryStore (in-process, for tests). Both are
// exercised by the same conformance suite in store_test.go.
//
// Every dedup query below only considers scripts whose status is
// story.StatusAccepted — a rejected draft didn't become a real story, so its
// axis choices stay available for a future attempt. Content-level dedup
// (used_names/used_places/used_aphorisms) is recorded explicitly via
// RecordAcceptance, called once, right when internal/generate accepts a
// script — never automatically, so a re-save of a still-pending script
// never double-books its content.
package store

import (
	"context"
	"time"

	"github.com/placeholder/scenario/internal/story"
)

// ListFilter narrows ListScripts. A zero value lists everything.
type ListFilter struct {
	Status story.Status // "" = any status
	Limit  int          // <= 0 = no limit
}

// ScriptSummary is the compact row ListScripts returns — enough for `gen
// list` to render a table without loading every chapter.
type ScriptSummary struct {
	ID          string
	CreatedAt   time.Time
	Title       string
	Profession  string
	Status      story.Status
	WordCount   int
	QualityMean float64 // 0 if the script has never been scored
}

// Run records one `gen generate` invocation, successful or not — the raw
// material for `gen stats`.
type Run struct {
	ID         string
	ScriptID   string // "" if generation failed before a script existed
	Provider   string
	Outcome    string // "accepted" | "rejected" | "error"
	Error      string // "" unless Outcome == "error"
	StartedAt  time.Time
	FinishedAt time.Time
}

// Stats aggregates across every stored script, for `gen stats`.
type Stats struct {
	TotalScripts    int
	AcceptedScripts int
	RejectedScripts int
	PendingScripts  int
	// AcceptedWithWarningsScripts is story.StatusAcceptedWithWarnings — the
	// whole-script repetition-guard validation never fully converged
	// within its round budget, but the script was kept rather than
	// discarded. Not counted in AcceptedScripts: it skipped review
	// entirely, so "accepted" would overstate what's actually been vetted.
	AcceptedWithWarningsScripts int
	AverageQuality              float64 // mean of QualityScores.Mean() across scored scripts
	AverageWordCount            float64
	// Usage aggregates every script's Usage entries by (role, provider,
	// model, cause) — the source `gen stats` reads for its cost/token/
	// thinking/repair-vs-initial breakdown across every run, not just the
	// most recent one.
	Usage []story.UsageEntry
}

// Store is everything internal/seed, internal/generate, and cmd/gen need
// from persistence. It is intentionally a single flat interface — small
// enough that MemoryStore and the real SQLite backend can both implement it
// completely, with no partial adapters.
type Store interface {
	// Seed-axis dedup (internal/seed.History) — accepted scripts only.
	RecentProfessions(ctx context.Context, n int) ([]string, error)
	TripletExists(ctx context.Context, profession, antagonist, endingType string) (bool, error)
	LastObjectContainer(ctx context.Context) (string, bool, error)
	LastDuration(ctx context.Context) (int, bool, error)
	LastReckoningPlace(ctx context.Context) (string, bool, error)
	// RecentLegacyArtifacts returns the last n accepted scripts'
	// legacy_artifact values, most-recent-first — unlike LastObjectContainer
	// (which only ever forbids repeating the immediately previous value),
	// legacy_artifact forces alternation across a short window instead of
	// just one step, since a real run drew the same value (a child's wax
	// crayon drawing) twice in a row.
	RecentLegacyArtifacts(ctx context.Context, n int) ([]string, error)

	// Content-level dedup (addendum) — accepted scripts only.
	RecentUsedNames(ctx context.Context, limit int) ([]string, error)
	RecentUsedPlaces(ctx context.Context, limit int) ([]string, error)
	RecentUsedAphorisms(ctx context.Context, limit int) ([]string, error)
	// TopUsedPhrases returns the `limit` six-word phrases most repeated
	// across the last `scripts` accepted scripts, ranked by how many of
	// those scripts used each one — the material for the chapter prompt's
	// "never use these exact phrasings" guidance.
	TopUsedPhrases(ctx context.Context, scripts, limit int) ([]string, error)
	// PhraseOverlaps checks phrases against the last `scripts` accepted
	// scripts' recorded used_phrases and returns, for every phrase that
	// matches at least one of them, the script ID(s) it matches — omitting
	// any phrase with no match at all. internal/generate's cross-script
	// repetition check aggregates the result per script ID: more than a
	// handful shared with any single prior script is the tell of the model
	// falling into the same phrasing twice, not coincidental overlap.
	PhraseOverlaps(ctx context.Context, phrases []string, scripts int) (map[string][]string, error)
	// RecordAcceptance books every character name, the narrator's town, the
	// FamilyLaw aphorism, and every distinct six-word phrase in the
	// script's text against future dedup queries above. Call this exactly
	// once, when a script's status is set to accepted.
	RecordAcceptance(ctx context.Context, script *story.Script) error
	// CountRecordedScripts counts scripts that reached a recorded status
	// (accepted or accepted_with_warnings) — the monotonic index the close
	// beat rotates its closing-image archetype by, so consecutive scripts
	// never land on the same final image.
	CountRecordedScripts(ctx context.Context) (int, error)

	// Core persistence. SaveScript is one transaction: script row, seed,
	// bible, and every chapter, all-or-nothing.
	SaveScript(ctx context.Context, script *story.Script) error
	GetScript(ctx context.Context, id string) (*story.Script, error)
	ListScripts(ctx context.Context, filter ListFilter) ([]ScriptSummary, error)
	// UpdateChapter replaces a single chapter's content — the write path for
	// `gen regenerate --chapter N` and continuity's targeted fixes, so a
	// one-chapter fix doesn't require resaving the whole script.
	UpdateChapter(ctx context.Context, scriptID string, chapter story.Chapter) error
	// SaveQualityScores appends one review attempt; GetScript and
	// ListScripts always reflect the latest attempt for a script.
	SaveQualityScores(ctx context.Context, scriptID string, scores story.QualityScores) error
	SetStatus(ctx context.Context, scriptID string, status story.Status) error

	SaveRun(ctx context.Context, run Run) error
	Stats(ctx context.Context) (Stats, error)

	Close() error
}
