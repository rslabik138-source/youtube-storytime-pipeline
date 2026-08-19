package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type sqliteStore struct {
	db *sql.DB
}

// Open opens (creating if necessary) a SQLite database at path and applies
// every pending migration. path can be a file path or ":memory:" for a
// short-lived store.
func Open(path string) (Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // single-writer: sidesteps SQLITE_BUSY entirely, no retry logic needed

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &sqliteStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (filename TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("store: read migrations dir: %w", err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	sort.Strings(names)

	for _, name := range names {
		var applied int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE filename = ?`, name).Scan(&applied); err != nil {
			return fmt.Errorf("store: check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", name, err)
		}
		if _, err := db.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("store: apply migration %s: %w", name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (filename, applied_at) VALUES (?, ?)`,
			name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("store: record migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

func (s *sqliteStore) RecentUsedVoices(ctx context.Context, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT voice FROM voiceovers ORDER BY created_at DESC, rowid DESC LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("store: recent used voices: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("store: scan recent used voice: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *sqliteStore) RecordVoiceover(ctx context.Context, v VoiceoverSummary) error {
	createdAt := v.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO voiceovers (script_id, voice, created_at, total_seconds, size_bytes)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(script_id) DO UPDATE SET
			voice=excluded.voice, created_at=excluded.created_at,
			total_seconds=excluded.total_seconds, size_bytes=excluded.size_bytes`,
		v.ScriptID, v.Voice, createdAt.UTC().Format(time.RFC3339Nano), v.TotalSeconds, v.SizeBytes)
	if err != nil {
		return fmt.Errorf("store: record voiceover: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetVoiceover(ctx context.Context, scriptID string) (VoiceoverSummary, error) {
	var v VoiceoverSummary
	var createdAt string
	row := s.db.QueryRowContext(ctx, `
		SELECT script_id, voice, created_at, total_seconds, size_bytes FROM voiceovers WHERE script_id = ?`, scriptID)
	if err := row.Scan(&v.ScriptID, &v.Voice, &createdAt, &v.TotalSeconds, &v.SizeBytes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VoiceoverSummary{}, fmt.Errorf("store: voiceover %q: %w", scriptID, ErrNotFound)
		}
		return VoiceoverSummary{}, fmt.Errorf("store: get voiceover: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return VoiceoverSummary{}, fmt.Errorf("store: parse created_at: %w", err)
	}
	v.CreatedAt = t
	return v, nil
}

func (s *sqliteStore) ListVoiceovers(ctx context.Context) ([]VoiceoverSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT script_id, voice, created_at, total_seconds, size_bytes
		FROM voiceovers ORDER BY created_at DESC, rowid DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: list voiceovers: %w", err)
	}
	defer rows.Close()

	var out []VoiceoverSummary
	for rows.Next() {
		var v VoiceoverSummary
		var createdAt string
		if err := rows.Scan(&v.ScriptID, &v.Voice, &createdAt, &v.TotalSeconds, &v.SizeBytes); err != nil {
			return nil, fmt.Errorf("store: scan voiceover: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("store: parse created_at: %w", err)
		}
		v.CreatedAt = t
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *sqliteStore) Stats(ctx context.Context) (StatsSummary, error) {
	var st StatsSummary
	row := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(total_seconds), 0), COALESCE(SUM(size_bytes), 0) FROM voiceovers`)
	if err := row.Scan(&st.TotalVoiceovers, &st.TotalSeconds, &st.TotalSizeBytes); err != nil {
		return StatsSummary{}, fmt.Errorf("store: stats: %w", err)
	}
	return st, nil
}
