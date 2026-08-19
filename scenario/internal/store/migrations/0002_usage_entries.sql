-- Per-(role, provider, model) token/cost breakdown for a script — lets
-- `gen stats` show where the bill actually went (e.g. most of it as
-- thinking tokens on chapter generation, not review), which the
-- scripts.tokens_in/tokens_out running totals alone can't distinguish.
CREATE TABLE usage_entries (
	script_id       TEXT NOT NULL REFERENCES scripts(id),
	role            TEXT NOT NULL,
	provider        TEXT NOT NULL,
	model           TEXT NOT NULL,
	calls           INTEGER NOT NULL,
	tokens_in       INTEGER NOT NULL,
	tokens_out      INTEGER NOT NULL,
	thinking_tokens INTEGER NOT NULL,
	PRIMARY KEY (script_id, role, provider, model)
);
