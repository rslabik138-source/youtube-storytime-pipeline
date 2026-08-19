CREATE TABLE voiceovers (
	script_id     TEXT PRIMARY KEY,
	voice         TEXT NOT NULL,
	created_at    TEXT NOT NULL,
	total_seconds REAL NOT NULL,
	size_bytes    INTEGER NOT NULL
);

CREATE INDEX idx_voiceovers_created_at ON voiceovers(created_at);
