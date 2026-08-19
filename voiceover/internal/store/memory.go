package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests — no filesystem, no cgo,
// same upsert-by-script-ID and ordering semantics as the SQLite backend.
type MemoryStore struct {
	mu   sync.Mutex
	rows map[string]VoiceoverSummary
	seq  map[string]int
	next int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[string]VoiceoverSummary{}, seq: map[string]int{}}
}

// sortedDesc returns every recorded voiceover, most-recent-first (ties
// broken by insertion order, newest first) — mirrors the SQL backend's
// `ORDER BY created_at DESC, rowid DESC`. Caller must hold m.mu.
func (m *MemoryStore) sortedDesc() []VoiceoverSummary {
	out := make([]VoiceoverSummary, 0, len(m.rows))
	for _, v := range m.rows {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := out[i].CreatedAt, out[j].CreatedAt
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return m.seq[out[i].ScriptID] > m.seq[out[j].ScriptID]
	})
	return out
}

func (m *MemoryStore) RecentUsedVoices(ctx context.Context, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	all := m.sortedDesc()
	if len(all) > n {
		all = all[:n]
	}
	out := make([]string, len(all))
	for i, v := range all {
		out[i] = v.Voice
	}
	return out, nil
}

func (m *MemoryStore) RecordVoiceover(ctx context.Context, v VoiceoverSummary) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	if _, ok := m.rows[v.ScriptID]; !ok {
		m.next++
		m.seq[v.ScriptID] = m.next
	}
	m.rows[v.ScriptID] = v
	return nil
}

func (m *MemoryStore) GetVoiceover(ctx context.Context, scriptID string) (VoiceoverSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	v, ok := m.rows[scriptID]
	if !ok {
		return VoiceoverSummary{}, fmt.Errorf("store: voiceover %q: %w", scriptID, ErrNotFound)
	}
	return v, nil
}

func (m *MemoryStore) ListVoiceovers(ctx context.Context) ([]VoiceoverSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sortedDesc(), nil
}

func (m *MemoryStore) Stats(ctx context.Context) (StatsSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var st StatsSummary
	for _, v := range m.rows {
		st.TotalVoiceovers++
		st.TotalSeconds += v.TotalSeconds
		st.TotalSizeBytes += v.SizeBytes
	}
	return st, nil
}

func (m *MemoryStore) Close() error { return nil }
