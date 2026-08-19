-- usage_entries.cause distinguishes "initial" (first-pass sequential
-- generation) from "repair" (continuity/repetition-guard/review-flagged
-- regeneration) spend on the exact same (role, provider, model) — real
-- runs showed scripts burning real money on repeated chapter regeneration
-- for scripts that never even got accepted; this is what makes that
-- visible in `gen stats` instead of hiding inside one number.
--
-- SQLite can't alter a PRIMARY KEY in place, so the table is recreated
-- with the widened key (role, provider, model, cause) can have separate
-- initial/repair rows for the same script instead of colliding. Existing
-- rows predate this distinction entirely, so they default to 'initial'
-- (a slight overcount of initial spend for old scripts, not a loss of
-- data — there's no way to retroactively know which of those calls were
-- repairs).
CREATE TABLE usage_entries_new (
	script_id       TEXT NOT NULL REFERENCES scripts(id),
	role            TEXT NOT NULL,
	provider        TEXT NOT NULL,
	model           TEXT NOT NULL,
	cause           TEXT NOT NULL DEFAULT 'initial',
	calls           INTEGER NOT NULL,
	tokens_in       INTEGER NOT NULL,
	tokens_out      INTEGER NOT NULL,
	thinking_tokens INTEGER NOT NULL,
	PRIMARY KEY (script_id, role, provider, model, cause)
);

INSERT INTO usage_entries_new (script_id, role, provider, model, cause, calls, tokens_in, tokens_out, thinking_tokens)
	SELECT script_id, role, provider, model, 'initial', calls, tokens_in, tokens_out, thinking_tokens FROM usage_entries;

DROP TABLE usage_entries;
ALTER TABLE usage_entries_new RENAME TO usage_entries;
