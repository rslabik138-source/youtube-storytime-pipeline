CREATE TABLE scripts (
	id         TEXT PRIMARY KEY,
	created_at TEXT NOT NULL,
	title      TEXT NOT NULL,
	status     TEXT NOT NULL,
	word_count INTEGER NOT NULL,
	provider   TEXT NOT NULL,
	model      TEXT NOT NULL,
	tokens_in  INTEGER NOT NULL,
	tokens_out INTEGER NOT NULL
);

CREATE TABLE seeds (
	script_id         TEXT PRIMARY KEY REFERENCES scripts(id),
	profession        TEXT NOT NULL,
	epistemology      TEXT NOT NULL,
	record_type       TEXT NOT NULL,
	protagonist_sex   TEXT NOT NULL,
	antagonist        TEXT NOT NULL,
	weak_ally         TEXT NOT NULL,
	humiliation_type  TEXT NOT NULL,
	betrayal          TEXT NOT NULL,
	duration          INTEGER NOT NULL,
	written_overreach TEXT NOT NULL,
	object_container  TEXT NOT NULL,
	legacy_artifact   TEXT NOT NULL,
	reckoning_place   TEXT NOT NULL,
	ending_type       TEXT NOT NULL,
	region            TEXT NOT NULL
);

CREATE TABLE bibles (
	script_id      TEXT PRIMARY KEY REFERENCES scripts(id),
	narrator_json  TEXT NOT NULL,
	cast_json      TEXT NOT NULL,
	timeline_json  TEXT NOT NULL,
	family_law     TEXT NOT NULL,
	refrain_phrase TEXT NOT NULL,
	seeded_line    TEXT NOT NULL,
	numbers_json   TEXT NOT NULL
);

CREATE TABLE chapters (
	script_id    TEXT NOT NULL REFERENCES scripts(id),
	idx          INTEGER NOT NULL,
	beat         TEXT NOT NULL,
	target_words INTEGER NOT NULL,
	text         TEXT NOT NULL,
	display_text TEXT NOT NULL,
	summary      TEXT NOT NULL,
	PRIMARY KEY (script_id, idx)
);

CREATE TABLE quality_scores (
	script_id             TEXT NOT NULL REFERENCES scripts(id),
	attempt               INTEGER NOT NULL,
	hook_strength         REAL NOT NULL,
	profession_causality  REAL NOT NULL,
	restraint             REAL NOT NULL,
	scene_not_summary     REAL NOT NULL,
	planting_payoff       REAL NOT NULL,
	refusal_present       REAL NOT NULL,
	ai_smell              REAL NOT NULL,
	comment               TEXT NOT NULL,
	created_at            TEXT NOT NULL,
	PRIMARY KEY (script_id, attempt)
);

CREATE TABLE runs (
	id          TEXT PRIMARY KEY,
	script_id   TEXT NOT NULL,
	provider    TEXT NOT NULL,
	outcome     TEXT NOT NULL,
	error       TEXT NOT NULL,
	started_at  TEXT NOT NULL,
	finished_at TEXT NOT NULL
);

CREATE TABLE used_names (
	name       TEXT NOT NULL,
	script_id  TEXT NOT NULL REFERENCES scripts(id),
	created_at TEXT NOT NULL
);

CREATE TABLE used_places (
	place      TEXT NOT NULL,
	script_id  TEXT NOT NULL REFERENCES scripts(id),
	created_at TEXT NOT NULL
);

CREATE TABLE used_aphorisms (
	aphorism   TEXT NOT NULL,
	script_id  TEXT NOT NULL REFERENCES scripts(id),
	created_at TEXT NOT NULL
);

CREATE INDEX idx_scripts_status_created_at ON scripts(status, created_at);
CREATE INDEX idx_used_names_created_at ON used_names(created_at);
CREATE INDEX idx_used_places_created_at ON used_places(created_at);
CREATE INDEX idx_used_aphorisms_created_at ON used_aphorisms(created_at);
