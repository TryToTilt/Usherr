-- Home fork: StashDB tags on scenes (JSON array of names). Performer genders
-- live in the existing performers JSON. details_at is when the scene's
-- details were last read from StashDB (empty = never, e.g. a Whisparr import).
ALTER TABLE scenes ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE scenes ADD COLUMN details_at TEXT NOT NULL DEFAULT '';
