-- Cross-script phrase dedup (addendum to used_names/used_places/
-- used_aphorisms): one row per distinct six-word phrase per accepted
-- script, so a later script can be checked against what earlier ones
-- already used — a real comparison of two back-to-back generated scripts
-- found 17 shared six-word phrases, including a verbatim repeated
-- sentence, that story.ValidateScript (which only ever sees one script at
-- a time) has no way to catch.
CREATE TABLE used_phrases (
	phrase     TEXT NOT NULL,
	script_id  TEXT NOT NULL REFERENCES scripts(id),
	created_at TEXT NOT NULL
);

CREATE INDEX idx_used_phrases_created_at ON used_phrases(created_at);
CREATE INDEX idx_used_phrases_phrase ON used_phrases(phrase);
