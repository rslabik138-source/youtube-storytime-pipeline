package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/placeholder/scenario/internal/story"
)

type memoryScript struct {
	script *story.Script         // owned by the store; always accessed through a clone
	scores []story.QualityScores // review attempt history, index 0 = attempt 1
	seq    int                   // assigned once, on first save — the rowid-equivalent tiebreaker
}

type usedEntry struct {
	value     string
	createdAt time.Time
}

// usedPhraseEntry mirrors usedEntry but also carries which script recorded
// it — unlike names/places/aphorisms, phrase dedup needs to know not just
// "was this used recently" but "which specific prior script(s) used it",
// since the cross-script check flags a script only once it shares more
// than a handful of phrases with any single one of them.
type usedPhraseEntry struct {
	phrase    string
	scriptID  string
	createdAt time.Time
}

// MemoryStore is an in-process Store for tests — no filesystem, no cgo,
// same dedup semantics as the SQLite backend (accepted scripts only).
type MemoryStore struct {
	mu      sync.Mutex
	nextSeq int
	scripts map[string]*memoryScript
	runs    map[string]Run

	usedNames   []usedEntry
	usedPlaces  []usedEntry
	usedAph     []usedEntry
	usedPhrases []usedPhraseEntry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		scripts: map[string]*memoryScript{},
		runs:    map[string]Run{},
	}
}

func cloneScript(s *story.Script) *story.Script {
	c := *s
	c.Chapters = append([]story.Chapter(nil), s.Chapters...)
	c.Bible = cloneBible(s.Bible)
	c.Usage = append([]story.UsageEntry(nil), s.Usage...)
	return &c
}

func cloneBible(b story.Bible) story.Bible {
	c := b
	c.Cast = append([]story.Person(nil), b.Cast...)
	c.Timeline = append([]story.Event(nil), b.Timeline...)
	if b.Numbers != nil {
		c.Numbers = make(map[string]string, len(b.Numbers))
		for k, v := range b.Numbers {
			c.Numbers[k] = v
		}
	}
	return c
}

func (m *MemoryStore) CountRecordedScripts(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, ms := range m.scripts {
		if ms.script.Status == story.StatusAccepted || ms.script.Status == story.StatusAcceptedWithWarnings {
			n++
		}
	}
	return n, nil
}

// isRecorded reports whether a script's status counts as a produced story
// for cross-script dedup — a plain accept OR an accept-with-warnings (which
// is still kept, exported, and rendered into a video) — but not a
// pending/rejected draft. Mirrors sqlite's
// `status IN ('accepted', 'accepted_with_warnings')`.
func isRecorded(s story.Status) bool {
	return s == story.StatusAccepted || s == story.StatusAcceptedWithWarnings
}

func (m *MemoryStore) acceptedSortedDesc() []*memoryScript {
	var out []*memoryScript
	for _, ms := range m.scripts {
		if isRecorded(ms.script.Status) {
			out = append(out, ms)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := out[i].script.CreatedAt, out[j].script.CreatedAt
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].seq > out[j].seq
	})
	return out
}

func (m *MemoryStore) RecentProfessions(ctx context.Context, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	accepted := m.acceptedSortedDesc()
	if len(accepted) > n {
		accepted = accepted[:n]
	}
	out := make([]string, len(accepted))
	for i, ms := range accepted {
		out[i] = ms.script.Seed.Profession
	}
	return out, nil
}

func (m *MemoryStore) TripletExists(ctx context.Context, profession, antagonist, endingType string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ms := range m.scripts {
		if !isRecorded(ms.script.Status) {
			continue
		}
		sd := ms.script.Seed
		if sd.Profession == profession && sd.Antagonist == antagonist && sd.EndingType == endingType {
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryStore) LastObjectContainer(ctx context.Context) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	accepted := m.acceptedSortedDesc()
	if len(accepted) == 0 {
		return "", false, nil
	}
	return accepted[0].script.Seed.ObjectContainer, true, nil
}

func (m *MemoryStore) LastDuration(ctx context.Context) (int, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	accepted := m.acceptedSortedDesc()
	if len(accepted) == 0 {
		return 0, false, nil
	}
	return accepted[0].script.Seed.Duration, true, nil
}

func (m *MemoryStore) LastReckoningPlace(ctx context.Context) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	accepted := m.acceptedSortedDesc()
	if len(accepted) == 0 {
		return "", false, nil
	}
	return accepted[0].script.Seed.ReckoningPlace, true, nil
}

func (m *MemoryStore) RecentLegacyArtifacts(ctx context.Context, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	accepted := m.acceptedSortedDesc()
	if len(accepted) > n {
		accepted = accepted[:n]
	}
	out := make([]string, len(accepted))
	for i, ms := range accepted {
		out[i] = ms.script.Seed.LegacyArtifact
	}
	return out, nil
}

func recentValues(entries []usedEntry, limit int) []string {
	if limit <= 0 {
		return nil
	}
	n := len(entries)
	start := n - limit
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, n-start)
	for i := n - 1; i >= start; i-- {
		out = append(out, entries[i].value)
	}
	return out
}

func (m *MemoryStore) RecentUsedNames(ctx context.Context, limit int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return recentValues(m.usedNames, limit), nil
}

func (m *MemoryStore) RecentUsedPlaces(ctx context.Context, limit int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return recentValues(m.usedPlaces, limit), nil
}

func (m *MemoryStore) RecentUsedAphorisms(ctx context.Context, limit int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return recentValues(m.usedAph, limit), nil
}

func (m *MemoryStore) RecordAcceptance(ctx context.Context, script *story.Script) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	names := append([]string{script.Bible.Narrator.Name}, namesOf(script.Bible.Cast)...)
	seenName := map[string]bool{}
	for _, n := range names {
		// Book the full name AND each part (first, last, ...) so a reused
		// surname is caught even when full names differ. See nameForms.
		for _, form := range nameForms(n) {
			if seenName[form] {
				continue
			}
			seenName[form] = true
			m.usedNames = append(m.usedNames, usedEntry{value: form, createdAt: now})
		}
	}
	if script.Bible.Narrator.City != "" {
		m.usedPlaces = append(m.usedPlaces, usedEntry{value: script.Bible.Narrator.City, createdAt: now})
	}
	if script.Bible.FamilyLaw != "" {
		m.usedAph = append(m.usedAph, usedEntry{value: script.Bible.FamilyLaw, createdAt: now})
	}
	for gram := range story.SixGramOccurrences(script) {
		m.usedPhrases = append(m.usedPhrases, usedPhraseEntry{phrase: gram, scriptID: script.ID, createdAt: now})
	}
	return nil
}

// recentAcceptedIDs returns the IDs of the last n accepted scripts,
// most-recent first.
func (m *MemoryStore) recentAcceptedIDs(n int) map[string]bool {
	accepted := m.acceptedSortedDesc()
	if len(accepted) > n {
		accepted = accepted[:n]
	}
	out := make(map[string]bool, len(accepted))
	for _, ms := range accepted {
		out[ms.script.ID] = true
	}
	return out
}

func (m *MemoryStore) TopUsedPhrases(ctx context.Context, scripts, limit int) ([]string, error) {
	if scripts <= 0 || limit <= 0 {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	recentIDs := m.recentAcceptedIDs(scripts)
	counts := map[string]int{}
	for _, e := range m.usedPhrases {
		if recentIDs[e.scriptID] {
			counts[e.phrase]++
		}
	}

	type kv struct {
		phrase string
		n      int
	}
	list := make([]kv, 0, len(counts))
	for p, n := range counts {
		list = append(list, kv{p, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].phrase < list[j].phrase
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.phrase
	}
	return out, nil
}

func (m *MemoryStore) PhraseOverlaps(ctx context.Context, phrases []string, scripts int) (map[string][]string, error) {
	if len(phrases) == 0 || scripts <= 0 {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	recentIDs := m.recentAcceptedIDs(scripts)
	want := make(map[string]bool, len(phrases))
	for _, p := range phrases {
		want[p] = true
	}

	out := map[string][]string{}
	for _, e := range m.usedPhrases {
		if !recentIDs[e.scriptID] || !want[e.phrase] {
			continue
		}
		out[e.phrase] = append(out[e.phrase], e.scriptID)
	}
	return out, nil
}

func (m *MemoryStore) SaveScript(ctx context.Context, script *story.Script) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	seq := m.nextSeq
	var scores []story.QualityScores
	if existing, ok := m.scripts[script.ID]; ok {
		seq = existing.seq
		scores = existing.scores
	} else {
		m.nextSeq++
	}

	clone := cloneScript(script)
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = time.Now().UTC()
	}
	m.scripts[script.ID] = &memoryScript{script: clone, scores: scores, seq: seq}
	return nil
}

func (m *MemoryStore) GetScript(ctx context.Context, id string) (*story.Script, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ms, ok := m.scripts[id]
	if !ok {
		return nil, fmt.Errorf("store: script %q: %w", id, ErrNotFound)
	}
	out := cloneScript(ms.script)
	if len(ms.scores) > 0 {
		out.Quality = ms.scores[len(ms.scores)-1]
	} else {
		out.Quality = story.QualityScores{}
	}
	return out, nil
}

func (m *MemoryStore) ListScripts(ctx context.Context, filter ListFilter) ([]ScriptSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var all []*memoryScript
	for _, ms := range m.scripts {
		if filter.Status != "" && ms.script.Status != filter.Status {
			continue
		}
		all = append(all, ms)
	}
	sort.Slice(all, func(i, j int) bool {
		ti, tj := all[i].script.CreatedAt, all[j].script.CreatedAt
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return all[i].seq > all[j].seq
	})
	if filter.Limit > 0 && len(all) > filter.Limit {
		all = all[:filter.Limit]
	}

	out := make([]ScriptSummary, len(all))
	for i, ms := range all {
		var mean float64
		if len(ms.scores) > 0 {
			mean = ms.scores[len(ms.scores)-1].Mean()
		}
		out[i] = ScriptSummary{
			ID:          ms.script.ID,
			CreatedAt:   ms.script.CreatedAt,
			Title:       ms.script.Title,
			Profession:  ms.script.Seed.Profession,
			Status:      ms.script.Status,
			WordCount:   ms.script.WordCount,
			QualityMean: mean,
		}
	}
	return out, nil
}

func (m *MemoryStore) UpdateChapter(ctx context.Context, scriptID string, chapter story.Chapter) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ms, ok := m.scripts[scriptID]
	if !ok {
		return fmt.Errorf("store: script %q: %w", scriptID, ErrNotFound)
	}
	for i, ch := range ms.script.Chapters {
		if ch.Index == chapter.Index {
			ms.script.Chapters[i] = chapter
			return nil
		}
	}
	ms.script.Chapters = append(ms.script.Chapters, chapter)
	sort.Slice(ms.script.Chapters, func(i, j int) bool { return ms.script.Chapters[i].Index < ms.script.Chapters[j].Index })
	return nil
}

func (m *MemoryStore) SaveQualityScores(ctx context.Context, scriptID string, scores story.QualityScores) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ms, ok := m.scripts[scriptID]
	if !ok {
		return fmt.Errorf("store: script %q: %w", scriptID, ErrNotFound)
	}
	ms.scores = append(ms.scores, scores)
	return nil
}

func (m *MemoryStore) SetStatus(ctx context.Context, scriptID string, status story.Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ms, ok := m.scripts[scriptID]
	if !ok {
		return fmt.Errorf("store: script %q: %w", scriptID, ErrNotFound)
	}
	ms.script.Status = status
	return nil
}

func (m *MemoryStore) SaveRun(ctx context.Context, run Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.ID] = run
	return nil
}

func (m *MemoryStore) Stats(ctx context.Context) (Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var st Stats
	var totalWords int
	var qualitySum float64
	var qualityCount int
	usageByKey := map[[4]string]*story.UsageEntry{}
	for _, ms := range m.scripts {
		st.TotalScripts++
		switch ms.script.Status {
		case story.StatusAccepted:
			st.AcceptedScripts++
		case story.StatusRejected:
			st.RejectedScripts++
		case story.StatusPending:
			st.PendingScripts++
		case story.StatusAcceptedWithWarnings:
			st.AcceptedWithWarningsScripts++
		}
		totalWords += ms.script.WordCount
		if len(ms.scores) > 0 {
			qualitySum += ms.scores[len(ms.scores)-1].Mean()
			qualityCount++
		}

		for _, u := range ms.script.Usage {
			key := [4]string{u.Role, u.Provider, u.Model, u.Cause}
			if existing, ok := usageByKey[key]; ok {
				existing.Calls += u.Calls
				existing.TokensIn += u.TokensIn
				existing.TokensOut += u.TokensOut
				existing.ThinkingTokens += u.ThinkingTokens
			} else {
				cp := u
				usageByKey[key] = &cp
			}
		}
	}
	if st.TotalScripts > 0 {
		st.AverageWordCount = float64(totalWords) / float64(st.TotalScripts)
	}
	if qualityCount > 0 {
		st.AverageQuality = qualitySum / float64(qualityCount)
	}
	for _, u := range usageByKey {
		st.Usage = append(st.Usage, *u)
	}
	return st, nil
}

func (m *MemoryStore) Close() error { return nil }
