-- Usherr: manga brought over from Comics. What was planned (shown to you first) and
-- what was done, step by step, so it can be undone. Files are only ever moved.
CREATE TABLE manga_moves (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    comic_series_id INTEGER NOT NULL,
    manga_series_id INTEGER NOT NULL DEFAULT 0,
    made_series     INTEGER NOT NULL DEFAULT 0,          -- the move added the manga series, so undo takes it away
    comic_name      TEXT NOT NULL DEFAULT '',
    title           TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'preview',     -- preview, applying, applied, undoing, undone, stopped
    plan            TEXT NOT NULL DEFAULT '{}',          -- JSON: what will happen, as you saw it
    journal         TEXT NOT NULL DEFAULT '{}',          -- JSON: what has happened
    done            INTEGER NOT NULL DEFAULT 0,
    total           INTEGER NOT NULL DEFAULT 0,
    problem         TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- A comic series that moved to Manga is hidden from Comics, not deleted, so a move
-- can be undone with its issues, ids and all.
ALTER TABLE comic_series ADD COLUMN moved_to_manga INTEGER NOT NULL DEFAULT 0;
