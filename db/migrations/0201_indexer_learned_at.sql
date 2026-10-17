-- When an indexer last told its daily limit (RFC 3339, empty = not known). A
-- learned limit is only trusted for a week without being heard again.
ALTER TABLE indexers ADD COLUMN learned_at TEXT NOT NULL DEFAULT '';
