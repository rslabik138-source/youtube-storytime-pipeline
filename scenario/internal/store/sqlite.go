package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/placeholder/scenario/internal/story"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned by GetScript and SetStatus when the requested
// script id doesn't exist.
var ErrNotFound = errors.New("store: not found")

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

func (s *sqliteStore) RecentProfessions(ctx context.Context, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT sd.profession FROM seeds sd
		JOIN scripts sc ON sc.id = sd.script_id
		WHERE sc.status IN (?, ?)
		ORDER BY sc.created_at DESC, sc.rowid DESC
		LIMIT ?`, string(story.StatusAccepted), string(story.StatusAcceptedWithWarnings), n)
	if err != nil {
		return nil, fmt.Errorf("store: recent professions: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("store: scan recent profession: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sqliteStore) TripletExists(ctx context.Context, profession, antagonist, endingType string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM seeds sd
		JOIN scripts sc ON sc.id = sd.script_id
		WHERE sc.status IN (?, ?) AND sd.profession = ? AND sd.antagonist = ? AND sd.ending_type = ?
		LIMIT 1`, string(story.StatusAccepted), string(story.StatusAcceptedWithWarnings), profession, antagonist, endingType).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: triplet exists: %w", err)
	}
	return true, nil
}

func (s *sqliteStore) LastObjectContainer(ctx context.Context) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `
		SELECT sd.object_container FROM seeds sd
		JOIN scripts sc ON sc.id = sd.script_id
		WHERE sc.status IN (?, ?)
		ORDER BY sc.created_at DESC, sc.rowid DESC
		LIMIT 1`, string(story.StatusAccepted), string(story.StatusAcceptedWithWarnings)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: last object container: %w", err)
	}
	return v, true, nil
}

func (s *sqliteStore) LastDuration(ctx context.Context) (int, bool, error) {
	var v int
	err := s.db.QueryRowContext(ctx, `
		SELECT sd.duration FROM seeds sd
		JOIN scripts sc ON sc.id = sd.script_id
		WHERE sc.status IN (?, ?)
		ORDER BY sc.created_at DESC, sc.rowid DESC
		LIMIT 1`, string(story.StatusAccepted), string(story.StatusAcceptedWithWarnings)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("store: last duration: %w", err)
	}
	return v, true, nil
}

func (s *sqliteStore) LastReckoningPlace(ctx context.Context) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `
		SELECT sd.reckoning_place FROM seeds sd
		JOIN scripts sc ON sc.id = sd.script_id
		WHERE sc.status IN (?, ?)
		ORDER BY sc.created_at DESC, sc.rowid DESC
		LIMIT 1`, string(story.StatusAccepted), string(story.StatusAcceptedWithWarnings)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: last reckoning place: %w", err)
	}
	return v, true, nil
}

func (s *sqliteStore) RecentLegacyArtifacts(ctx context.Context, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT sd.legacy_artifact FROM seeds sd
		JOIN scripts sc ON sc.id = sd.script_id
		WHERE sc.status IN (?, ?)
		ORDER BY sc.created_at DESC, sc.rowid DESC
		LIMIT ?`, string(story.StatusAccepted), string(story.StatusAcceptedWithWarnings), n)
	if err != nil {
		return nil, fmt.Errorf("store: recent legacy artifacts: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("store: scan recent legacy artifact: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *sqliteStore) RecentUsedNames(ctx context.Context, limit int) ([]string, error) {
	return s.recentStrings(ctx, "used_names", "name", limit)
}

func (s *sqliteStore) RecentUsedPlaces(ctx context.Context, limit int) ([]string, error) {
	return s.recentStrings(ctx, "used_places", "place", limit)
}

func (s *sqliteStore) RecentUsedAphorisms(ctx context.Context, limit int) ([]string, error) {
	return s.recentStrings(ctx, "used_aphorisms", "aphorism", limit)
}

// recentStrings is shared by the three used_* readers above. table and
// column are always one of the fixed internal literals passed by this
// file, never external input, so building the query with fmt.Sprintf here
// carries no injection risk.
func (s *sqliteStore) recentStrings(ctx context.Context, table, column string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	query := fmt.Sprintf(`SELECT %s FROM %s ORDER BY created_at DESC, rowid DESC LIMIT ?`, column, table)
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("store: recent %s: %w", table, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("store: scan %s: %w", table, err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *sqliteStore) TopUsedPhrases(ctx context.Context, scripts, limit int) ([]string, error) {
	if scripts <= 0 || limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT phrase, COUNT(*) AS n
		FROM used_phrases
		WHERE script_id IN (
			SELECT id FROM scripts WHERE status IN (?, ?) ORDER BY created_at DESC, rowid DESC LIMIT ?
		)
		GROUP BY phrase
		ORDER BY n DESC, phrase ASC
		LIMIT ?`, string(story.StatusAccepted), string(story.StatusAcceptedWithWarnings), scripts, limit)
	if err != nil {
		return nil, fmt.Errorf("store: top used phrases: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var phrase string
		var n int
		if err := rows.Scan(&phrase, &n); err != nil {
			return nil, fmt.Errorf("store: scan top used phrase: %w", err)
		}
		out = append(out, phrase)
	}
	return out, rows.Err()
}

// phraseOverlapBatchSize keeps each IN clause well under SQLite's default
// bound-parameter limit — a full script can have several thousand distinct
// six-word phrases, far more than fit in one query.
const phraseOverlapBatchSize = 300

func (s *sqliteStore) recentAcceptedScriptIDs(ctx context.Context, n int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM scripts WHERE status IN (?, ?) ORDER BY created_at DESC, rowid DESC LIMIT ?`,
		string(story.StatusAccepted), string(story.StatusAcceptedWithWarnings), n)
	if err != nil {
		return nil, fmt.Errorf("store: recent accepted script ids: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan recent accepted script id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *sqliteStore) PhraseOverlaps(ctx context.Context, phrases []string, scripts int) (map[string][]string, error) {
	if len(phrases) == 0 || scripts <= 0 {
		return nil, nil
	}
	scriptIDs, err := s.recentAcceptedScriptIDs(ctx, scripts)
	if err != nil {
		return nil, err
	}
	if len(scriptIDs) == 0 {
		return nil, nil
	}
	scriptPlaceholders := make([]string, len(scriptIDs))
	for i := range scriptIDs {
		scriptPlaceholders[i] = "?"
	}
	scriptPlaceholderClause := strings.Join(scriptPlaceholders, ",")

	out := map[string][]string{}
	for start := 0; start < len(phrases); start += phraseOverlapBatchSize {
		end := min(start+phraseOverlapBatchSize, len(phrases))
		batch := phrases[start:end]

		placeholders := make([]string, len(batch))
		args := make([]any, 0, len(batch)+len(scriptIDs))
		for i, p := range batch {
			placeholders[i] = "?"
			args = append(args, p)
		}
		for _, id := range scriptIDs {
			args = append(args, id)
		}

		query := fmt.Sprintf(`
			SELECT phrase, script_id FROM used_phrases
			WHERE phrase IN (%s) AND script_id IN (%s)`,
			strings.Join(placeholders, ","), scriptPlaceholderClause)

		if err := func() error {
			rows, err := s.db.QueryContext(ctx, query, args...)
			if err != nil {
				return fmt.Errorf("store: phrase overlaps: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var phrase, scriptID string
				if err := rows.Scan(&phrase, &scriptID); err != nil {
					return fmt.Errorf("store: scan phrase overlap: %w", err)
				}
				out[phrase] = append(out[phrase], scriptID)
			}
			return rows.Err()
		}(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// nameForms returns the full name plus each of its whitespace-separated
// parts (first, last, ...) as separate dedup keys, so a shared SURNAME is
// caught even when full names differ. A single-token name returns just
// itself. Empty/whitespace input returns nothing.
func nameForms(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	parts := strings.Fields(name)
	if len(parts) <= 1 {
		return []string{name}
	}
	return append([]string{name}, parts...)
}

func (s *sqliteStore) CountRecordedScripts(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM scripts WHERE status IN (?, ?)`,
		string(story.StatusAccepted), string(story.StatusAcceptedWithWarnings)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count recorded scripts: %w", err)
	}
	return n, nil
}

func (s *sqliteStore) RecordAcceptance(ctx context.Context, script *story.Script) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: record acceptance begin: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339Nano)

	names := append([]string{script.Bible.Narrator.Name}, namesOf(script.Bible.Cast)...)
	seenName := map[string]bool{}
	for _, n := range names {
		// Book the full name AND each of its parts (first, last, ...) as
		// separate rows, so a reused SURNAME is caught even when the full
		// names differ — two protagonists named "Caleb Vance" and "Julian
		// Vance" both book "Vance", and the next bible's name dedup sees it.
		for _, form := range nameForms(n) {
			if seenName[form] {
				continue
			}
			seenName[form] = true
			if _, err := tx.ExecContext(ctx, `INSERT INTO used_names (name, script_id, created_at) VALUES (?, ?, ?)`,
				form, script.ID, now); err != nil {
				return fmt.Errorf("store: insert used_names: %w", err)
			}
		}
	}

	if script.Bible.Narrator.City != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO used_places (place, script_id, created_at) VALUES (?, ?, ?)`,
			script.Bible.Narrator.City, script.ID, now); err != nil {
			return fmt.Errorf("store: insert used_places: %w", err)
		}
	}

	if script.Bible.FamilyLaw != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO used_aphorisms (aphorism, script_id, created_at) VALUES (?, ?, ?)`,
			script.Bible.FamilyLaw, script.ID, now); err != nil {
			return fmt.Errorf("store: insert used_aphorisms: %w", err)
		}
	}

	phraseStmt, err := tx.PrepareContext(ctx, `INSERT INTO used_phrases (phrase, script_id, created_at) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: prepare used_phrases insert: %w", err)
	}
	defer phraseStmt.Close()
	for gram := range story.SixGramOccurrences(script) {
		if _, err := phraseStmt.ExecContext(ctx, gram, script.ID, now); err != nil {
			return fmt.Errorf("store: insert used_phrases: %w", err)
		}
	}

	return tx.Commit()
}

func namesOf(people []story.Person) []string {
	out := make([]string, len(people))
	for i, p := range people {
		out[i] = p.Name
	}
	return out
}

func (s *sqliteStore) SaveScript(ctx context.Context, script *story.Script) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: save script begin: %w", err)
	}
	defer tx.Rollback()

	createdAt := script.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scripts (id, created_at, title, status, word_count, provider, model, tokens_in, tokens_out)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, status=excluded.status, word_count=excluded.word_count,
			provider=excluded.provider, model=excluded.model, tokens_in=excluded.tokens_in, tokens_out=excluded.tokens_out`,
		script.ID, createdAt.UTC().Format(time.RFC3339Nano), script.Title, string(script.Status),
		script.WordCount, script.Provider, script.Model, script.TokensIn, script.TokensOut); err != nil {
		return fmt.Errorf("store: upsert script: %w", err)
	}

	sd := script.Seed
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO seeds (script_id, profession, epistemology, record_type, protagonist_sex, antagonist, weak_ally,
			humiliation_type, betrayal, duration, written_overreach, object_container, legacy_artifact,
			reckoning_place, ending_type, region, attire)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(script_id) DO UPDATE SET
			profession=excluded.profession, epistemology=excluded.epistemology, record_type=excluded.record_type,
			protagonist_sex=excluded.protagonist_sex, antagonist=excluded.antagonist, weak_ally=excluded.weak_ally,
			humiliation_type=excluded.humiliation_type, betrayal=excluded.betrayal, duration=excluded.duration,
			written_overreach=excluded.written_overreach, object_container=excluded.object_container,
			legacy_artifact=excluded.legacy_artifact, reckoning_place=excluded.reckoning_place,
			ending_type=excluded.ending_type, region=excluded.region, attire=excluded.attire`,
		script.ID, sd.Profession, sd.Epistemology, sd.RecordType, sd.ProtagonistSex, sd.Antagonist, sd.WeakAlly,
		sd.HumiliationType, sd.Betrayal, sd.Duration, sd.WrittenOverreach, sd.ObjectContainer, sd.LegacyArtifact,
		sd.ReckoningPlace, sd.EndingType, sd.Region, sd.Attire); err != nil {
		return fmt.Errorf("store: upsert seed: %w", err)
	}

	narratorJSON, err := json.Marshal(script.Bible.Narrator)
	if err != nil {
		return fmt.Errorf("store: marshal narrator: %w", err)
	}
	castJSON, err := json.Marshal(script.Bible.Cast)
	if err != nil {
		return fmt.Errorf("store: marshal cast: %w", err)
	}
	timelineJSON, err := json.Marshal(script.Bible.Timeline)
	if err != nil {
		return fmt.Errorf("store: marshal timeline: %w", err)
	}
	numbersJSON, err := json.Marshal(script.Bible.Numbers)
	if err != nil {
		return fmt.Errorf("store: marshal numbers: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO bibles (script_id, narrator_json, cast_json, timeline_json, family_law, refrain_phrase, seeded_line, numbers_json)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(script_id) DO UPDATE SET
			narrator_json=excluded.narrator_json, cast_json=excluded.cast_json, timeline_json=excluded.timeline_json,
			family_law=excluded.family_law, refrain_phrase=excluded.refrain_phrase, seeded_line=excluded.seeded_line,
			numbers_json=excluded.numbers_json`,
		script.ID, string(narratorJSON), string(castJSON), string(timelineJSON),
		script.Bible.FamilyLaw, script.Bible.RefrainPhrase, script.Bible.SeededLine, string(numbersJSON)); err != nil {
		return fmt.Errorf("store: upsert bible: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM chapters WHERE script_id = ?`, script.ID); err != nil {
		return fmt.Errorf("store: clear chapters: %w", err)
	}
	for _, ch := range script.Chapters {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chapters (script_id, idx, beat, target_words, text, display_text, summary)
			VALUES (?,?,?,?,?,?,?)`,
			script.ID, ch.Index, ch.Beat, ch.TargetWords, ch.Text, ch.DisplayText, ch.Summary); err != nil {
			return fmt.Errorf("store: insert chapter %d: %w", ch.Index, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_entries WHERE script_id = ?`, script.ID); err != nil {
		return fmt.Errorf("store: clear usage entries: %w", err)
	}
	for _, u := range script.Usage {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage_entries (script_id, role, provider, model, cause, calls, tokens_in, tokens_out, thinking_tokens)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			script.ID, u.Role, u.Provider, u.Model, u.Cause, u.Calls, u.TokensIn, u.TokensOut, u.ThinkingTokens); err != nil {
			return fmt.Errorf("store: insert usage entry (role %s): %w", u.Role, err)
		}
	}

	return tx.Commit()
}

func (s *sqliteStore) GetScript(ctx context.Context, id string) (*story.Script, error) {
	var sc story.Script
	var createdAt, status string

	row := s.db.QueryRowContext(ctx, `
		SELECT id, created_at, title, status, word_count, provider, model, tokens_in, tokens_out
		FROM scripts WHERE id = ?`, id)
	if err := row.Scan(&sc.ID, &createdAt, &sc.Title, &status, &sc.WordCount, &sc.Provider, &sc.Model, &sc.TokensIn, &sc.TokensOut); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: script %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("store: get script: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("store: parse created_at: %w", err)
	}
	sc.CreatedAt = t
	sc.Status = story.Status(status)

	seedRow := s.db.QueryRowContext(ctx, `
		SELECT profession, epistemology, record_type, protagonist_sex, antagonist, weak_ally, humiliation_type,
			betrayal, duration, written_overreach, object_container, legacy_artifact, reckoning_place, ending_type, region, attire
		FROM seeds WHERE script_id = ?`, id)
	if err := seedRow.Scan(&sc.Seed.Profession, &sc.Seed.Epistemology, &sc.Seed.RecordType, &sc.Seed.ProtagonistSex,
		&sc.Seed.Antagonist, &sc.Seed.WeakAlly, &sc.Seed.HumiliationType, &sc.Seed.Betrayal, &sc.Seed.Duration,
		&sc.Seed.WrittenOverreach, &sc.Seed.ObjectContainer, &sc.Seed.LegacyArtifact, &sc.Seed.ReckoningPlace,
		&sc.Seed.EndingType, &sc.Seed.Region, &sc.Seed.Attire); err != nil {
		return nil, fmt.Errorf("store: get seed: %w", err)
	}

	var narratorJSON, castJSON, timelineJSON, numbersJSON string
	bibleRow := s.db.QueryRowContext(ctx, `
		SELECT narrator_json, cast_json, timeline_json, family_law, refrain_phrase, seeded_line, numbers_json
		FROM bibles WHERE script_id = ?`, id)
	if err := bibleRow.Scan(&narratorJSON, &castJSON, &timelineJSON, &sc.Bible.FamilyLaw, &sc.Bible.RefrainPhrase,
		&sc.Bible.SeededLine, &numbersJSON); err != nil {
		return nil, fmt.Errorf("store: get bible: %w", err)
	}
	if err := json.Unmarshal([]byte(narratorJSON), &sc.Bible.Narrator); err != nil {
		return nil, fmt.Errorf("store: unmarshal narrator: %w", err)
	}
	if err := json.Unmarshal([]byte(castJSON), &sc.Bible.Cast); err != nil {
		return nil, fmt.Errorf("store: unmarshal cast: %w", err)
	}
	if err := json.Unmarshal([]byte(timelineJSON), &sc.Bible.Timeline); err != nil {
		return nil, fmt.Errorf("store: unmarshal timeline: %w", err)
	}
	if err := json.Unmarshal([]byte(numbersJSON), &sc.Bible.Numbers); err != nil {
		return nil, fmt.Errorf("store: unmarshal numbers: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT idx, beat, target_words, text, display_text, summary
		FROM chapters WHERE script_id = ? ORDER BY idx ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("store: get chapters: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ch story.Chapter
		if err := rows.Scan(&ch.Index, &ch.Beat, &ch.TargetWords, &ch.Text, &ch.DisplayText, &ch.Summary); err != nil {
			return nil, fmt.Errorf("store: scan chapter: %w", err)
		}
		sc.Chapters = append(sc.Chapters, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: chapters rows: %w", err)
	}

	scores, err := s.latestQualityScores(ctx, id)
	if err != nil {
		return nil, err
	}
	sc.Quality = scores

	usageRows, err := s.db.QueryContext(ctx, `
		SELECT role, provider, model, cause, calls, tokens_in, tokens_out, thinking_tokens
		FROM usage_entries WHERE script_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("store: get usage entries: %w", err)
	}
	defer usageRows.Close()
	for usageRows.Next() {
		var u story.UsageEntry
		if err := usageRows.Scan(&u.Role, &u.Provider, &u.Model, &u.Cause, &u.Calls, &u.TokensIn, &u.TokensOut, &u.ThinkingTokens); err != nil {
			return nil, fmt.Errorf("store: scan usage entry: %w", err)
		}
		sc.Usage = append(sc.Usage, u)
	}
	if err := usageRows.Err(); err != nil {
		return nil, fmt.Errorf("store: usage entries rows: %w", err)
	}

	return &sc, nil
}

func (s *sqliteStore) latestQualityScores(ctx context.Context, scriptID string) (story.QualityScores, error) {
	var q story.QualityScores
	row := s.db.QueryRowContext(ctx, `
		SELECT hook_strength, profession_causality, restraint, scene_not_summary, planting_payoff, refusal_present, ai_smell, comment
		FROM quality_scores WHERE script_id = ? ORDER BY attempt DESC LIMIT 1`, scriptID)
	err := row.Scan(&q.HookStrength, &q.ProfessionCausality, &q.Restraint, &q.SceneNotSummary, &q.PlantingPayoff,
		&q.RefusalPresent, &q.AISmell, &q.Comment)
	if errors.Is(err, sql.ErrNoRows) {
		return story.QualityScores{}, nil
	}
	if err != nil {
		return story.QualityScores{}, fmt.Errorf("store: latest quality scores: %w", err)
	}
	return q, nil
}

func (s *sqliteStore) ListScripts(ctx context.Context, filter ListFilter) ([]ScriptSummary, error) {
	query := `
		SELECT sc.id, sc.created_at, sc.title, sc.status, sc.word_count, sd.profession
		FROM scripts sc JOIN seeds sd ON sd.script_id = sc.id`
	var args []any
	if filter.Status != "" {
		query += ` WHERE sc.status = ?`
		args = append(args, string(filter.Status))
	}
	query += ` ORDER BY sc.created_at DESC, sc.rowid DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list scripts: %w", err)
	}
	defer rows.Close()

	var out []ScriptSummary
	for rows.Next() {
		var sum ScriptSummary
		var createdAt, status string
		if err := rows.Scan(&sum.ID, &createdAt, &sum.Title, &status, &sum.WordCount, &sum.Profession); err != nil {
			return nil, fmt.Errorf("store: scan script summary: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("store: parse created_at: %w", err)
		}
		sum.CreatedAt = t
		sum.Status = story.Status(status)
		out = append(out, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list scripts rows: %w", err)
	}

	for i := range out {
		scores, err := s.latestQualityScores(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].QualityMean = scores.Mean()
	}
	return out, nil
}

func (s *sqliteStore) UpdateChapter(ctx context.Context, scriptID string, chapter story.Chapter) error {
	if err := s.requireScript(ctx, scriptID); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chapters (script_id, idx, beat, target_words, text, display_text, summary)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(script_id, idx) DO UPDATE SET
			beat=excluded.beat, target_words=excluded.target_words, text=excluded.text,
			display_text=excluded.display_text, summary=excluded.summary`,
		scriptID, chapter.Index, chapter.Beat, chapter.TargetWords, chapter.Text, chapter.DisplayText, chapter.Summary)
	if err != nil {
		return fmt.Errorf("store: update chapter %d: %w", chapter.Index, err)
	}
	return nil
}

// requireScript mirrors MemoryStore's "unknown script id" behavior — SQLite
// has no foreign_keys pragma enabled here, so without this check an insert
// against a chapter/quality_scores row could silently reference a script
// that was never saved.
func (s *sqliteStore) requireScript(ctx context.Context, scriptID string) error {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM scripts WHERE id = ?`, scriptID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("store: script %q: %w", scriptID, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("store: check script %q: %w", scriptID, err)
	}
	return nil
}

func (s *sqliteStore) SaveQualityScores(ctx context.Context, scriptID string, scores story.QualityScores) error {
	if err := s.requireScript(ctx, scriptID); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: save quality scores begin: %w", err)
	}
	defer tx.Rollback()

	var maxAttempt int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt), 0) FROM quality_scores WHERE script_id = ?`, scriptID).
		Scan(&maxAttempt); err != nil {
		return fmt.Errorf("store: max attempt: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO quality_scores (script_id, attempt, hook_strength, profession_causality, restraint,
			scene_not_summary, planting_payoff, refusal_present, ai_smell, comment, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		scriptID, maxAttempt+1, scores.HookStrength, scores.ProfessionCausality, scores.Restraint,
		scores.SceneNotSummary, scores.PlantingPayoff, scores.RefusalPresent, scores.AISmell, scores.Comment,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("store: insert quality scores: %w", err)
	}

	return tx.Commit()
}

func (s *sqliteStore) SetStatus(ctx context.Context, scriptID string, status story.Status) error {
	res, err := s.db.ExecContext(ctx, `UPDATE scripts SET status = ? WHERE id = ?`, string(status), scriptID)
	if err != nil {
		return fmt.Errorf("store: set status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set status rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: script %q: %w", scriptID, ErrNotFound)
	}
	return nil
}

func (s *sqliteStore) SaveRun(ctx context.Context, run Run) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO runs (id, script_id, provider, outcome, error, started_at, finished_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			script_id=excluded.script_id, provider=excluded.provider, outcome=excluded.outcome,
			error=excluded.error, started_at=excluded.started_at, finished_at=excluded.finished_at`,
		run.ID, run.ScriptID, run.Provider, run.Outcome, run.Error,
		run.StartedAt.UTC().Format(time.RFC3339Nano), run.FinishedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store: save run: %w", err)
	}
	return nil
}

func (s *sqliteStore) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(word_count), 0)
		FROM scripts`,
		string(story.StatusAccepted), string(story.StatusRejected), string(story.StatusPending), string(story.StatusAcceptedWithWarnings))
	if err := row.Scan(&st.TotalScripts, &st.AcceptedScripts, &st.RejectedScripts, &st.PendingScripts, &st.AcceptedWithWarningsScripts, &st.AverageWordCount); err != nil {
		return Stats{}, fmt.Errorf("store: stats: %w", err)
	}

	row2 := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(AVG(v), 0) FROM (
			SELECT (hook_strength+profession_causality+restraint+scene_not_summary+planting_payoff+refusal_present+ai_smell)/7.0 AS v
			FROM quality_scores qs
			WHERE qs.attempt = (SELECT MAX(attempt) FROM quality_scores WHERE script_id = qs.script_id)
		)`)
	if err := row2.Scan(&st.AverageQuality); err != nil {
		return Stats{}, fmt.Errorf("store: average quality: %w", err)
	}

	usageRows, err := s.db.QueryContext(ctx, `
		SELECT role, provider, model, cause, SUM(calls), SUM(tokens_in), SUM(tokens_out), SUM(thinking_tokens)
		FROM usage_entries
		GROUP BY role, provider, model, cause`)
	if err != nil {
		return Stats{}, fmt.Errorf("store: usage stats: %w", err)
	}
	defer usageRows.Close()
	for usageRows.Next() {
		var u story.UsageEntry
		if err := usageRows.Scan(&u.Role, &u.Provider, &u.Model, &u.Cause, &u.Calls, &u.TokensIn, &u.TokensOut, &u.ThinkingTokens); err != nil {
			return Stats{}, fmt.Errorf("store: scan usage stats: %w", err)
		}
		st.Usage = append(st.Usage, u)
	}
	if err := usageRows.Err(); err != nil {
		return Stats{}, fmt.Errorf("store: usage stats rows: %w", err)
	}

	return st, nil
}
