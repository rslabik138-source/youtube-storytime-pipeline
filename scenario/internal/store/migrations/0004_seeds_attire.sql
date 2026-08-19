-- Seed.Attire (the narrator's work clothing, resolved from
-- configs/professions.yaml at seed-draw time) was added to the Go struct
-- without a matching column, so it was silently dropped on every save —
-- caught via a real export whose manifest.json had every appearance field
-- populated except attire. Default '' matches scripts saved before this
-- column existed.
ALTER TABLE seeds ADD COLUMN attire TEXT NOT NULL DEFAULT '';
