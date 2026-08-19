package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func newMemoryBackend(t *testing.T) Store {
	t.Helper()
	return NewMemoryStore()
}

func newSQLiteBackend(t *testing.T) Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func withBothBackends(t *testing.T, run func(t *testing.T, s Store)) {
	t.Helper()
	for _, backend := range []struct {
		name    string
		factory func(t *testing.T) Store
	}{
		{"memory", newMemoryBackend},
		{"sqlite", newSQLiteBackend},
	} {
		t.Run(backend.name, func(t *testing.T) {
			run(t, backend.factory(t))
		})
	}
}

func TestRecordAndGetVoiceover(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		v := VoiceoverSummary{
			ScriptID: "s1", Voice: "af_bella",
			CreatedAt:    time.Now().UTC().Truncate(time.Second),
			TotalSeconds: 123.45, SizeBytes: 999,
		}
		if err := s.RecordVoiceover(ctx, v); err != nil {
			t.Fatalf("RecordVoiceover: %v", err)
		}

		got, err := s.GetVoiceover(ctx, "s1")
		if err != nil {
			t.Fatalf("GetVoiceover: %v", err)
		}
		if got.Voice != "af_bella" || got.TotalSeconds != 123.45 || got.SizeBytes != 999 {
			t.Fatalf("unexpected voiceover: %+v", got)
		}
		if !got.CreatedAt.Equal(v.CreatedAt) {
			t.Fatalf("expected CreatedAt to round-trip, got %v want %v", got.CreatedAt, v.CreatedAt)
		}
	})
}

func TestGetVoiceoverNotFoundReturnsErrNotFound(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		_, err := s.GetVoiceover(context.Background(), "missing")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestRecordVoiceoverUpsertsOnSameScriptID(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC().Truncate(time.Second)

		if err := s.RecordVoiceover(ctx, VoiceoverSummary{ScriptID: "s1", Voice: "af_bella", CreatedAt: base, TotalSeconds: 100, SizeBytes: 1}); err != nil {
			t.Fatalf("first record: %v", err)
		}
		if err := s.RecordVoiceover(ctx, VoiceoverSummary{ScriptID: "s1", Voice: "am_adam", CreatedAt: base.Add(time.Hour), TotalSeconds: 200, SizeBytes: 2}); err != nil {
			t.Fatalf("second record: %v", err)
		}

		got, err := s.GetVoiceover(ctx, "s1")
		if err != nil {
			t.Fatalf("GetVoiceover: %v", err)
		}
		if got.Voice != "am_adam" || got.TotalSeconds != 200 {
			t.Fatalf("expected the second record to overwrite the first, got %+v", got)
		}

		list, err := s.ListVoiceovers(ctx)
		if err != nil {
			t.Fatalf("ListVoiceovers: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("expected exactly 1 voiceover after upsert (not 2), got %d", len(list))
		}
	})
}

func TestRecentUsedVoicesMostRecentFirst(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC().Truncate(time.Second)
		voices := []string{"af_bella", "am_adam", "af_older"}
		for i, voice := range voices {
			if err := s.RecordVoiceover(ctx, VoiceoverSummary{
				ScriptID: fmt.Sprintf("s%d", i), Voice: voice, CreatedAt: base.Add(time.Duration(i) * time.Hour),
			}); err != nil {
				t.Fatalf("RecordVoiceover %d: %v", i, err)
			}
		}

		recent, err := s.RecentUsedVoices(ctx, 2)
		if err != nil {
			t.Fatalf("RecentUsedVoices: %v", err)
		}
		if len(recent) != 2 || recent[0] != "af_older" || recent[1] != "am_adam" {
			t.Fatalf("expected [af_older am_adam] (most recent first), got %v", recent)
		}

		limited, err := s.RecentUsedVoices(ctx, 1)
		if err != nil {
			t.Fatalf("RecentUsedVoices limit 1: %v", err)
		}
		if len(limited) != 1 || limited[0] != "af_older" {
			t.Fatalf("expected [af_older], got %v", limited)
		}
	})
}

func TestRecentUsedVoicesEmptyStoreReturnsNil(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		recent, err := s.RecentUsedVoices(context.Background(), 3)
		if err != nil || len(recent) != 0 {
			t.Fatalf("expected no recent voices, got %v err=%v", recent, err)
		}
	})
}

func TestListVoiceoversOrdersByMostRecentFirst(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC().Truncate(time.Second)
		if err := s.RecordVoiceover(ctx, VoiceoverSummary{ScriptID: "s1", Voice: "af_bella", CreatedAt: base}); err != nil {
			t.Fatalf("record s1: %v", err)
		}
		if err := s.RecordVoiceover(ctx, VoiceoverSummary{ScriptID: "s2", Voice: "am_adam", CreatedAt: base.Add(time.Hour)}); err != nil {
			t.Fatalf("record s2: %v", err)
		}

		list, err := s.ListVoiceovers(ctx)
		if err != nil {
			t.Fatalf("ListVoiceovers: %v", err)
		}
		if len(list) != 2 || list[0].ScriptID != "s2" || list[1].ScriptID != "s1" {
			t.Fatalf("expected [s2 s1] most-recent-first, got %+v", list)
		}
	})
}

func TestStatsAggregatesAcrossVoiceovers(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if err := s.RecordVoiceover(ctx, VoiceoverSummary{ScriptID: "s1", Voice: "af_bella", TotalSeconds: 100, SizeBytes: 1000}); err != nil {
			t.Fatalf("record s1: %v", err)
		}
		if err := s.RecordVoiceover(ctx, VoiceoverSummary{ScriptID: "s2", Voice: "am_adam", TotalSeconds: 200, SizeBytes: 2000}); err != nil {
			t.Fatalf("record s2: %v", err)
		}

		st, err := s.Stats(ctx)
		if err != nil {
			t.Fatalf("Stats: %v", err)
		}
		if st.TotalVoiceovers != 2 || st.TotalSeconds != 300 || st.TotalSizeBytes != 3000 {
			t.Fatalf("unexpected stats: %+v", st)
		}
	})
}

func TestStatsOnEmptyStore(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		st, err := s.Stats(context.Background())
		if err != nil {
			t.Fatalf("Stats: %v", err)
		}
		if st.TotalVoiceovers != 0 || st.TotalSeconds != 0 || st.TotalSizeBytes != 0 {
			t.Fatalf("expected zero stats, got %+v", st)
		}
	})
}
