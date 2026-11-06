-- Usherr: "Tidy up your music" (music_tidy.go, internal/musictidy), the
-- Picard-style rename and re-tag with a preview and undo. Modelled on
-- DroppedNeedle's library management plans, items and undo journal
-- (AGPL-3.0).

-- One "Check my music": the albums it looked at, and later what was applied
-- from it. status: preview, applied (some of its albums were tidied),
-- undone. recycled is 1 once its kept originals were emptied.
CREATE TABLE IF NOT EXISTS music_tidy_plans (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    root       TEXT    NOT NULL,
    status     TEXT    NOT NULL DEFAULT 'preview',
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    applied_at TEXT    NOT NULL DEFAULT '',
    undone_at  TEXT    NOT NULL DEFAULT '',
    recycled   INTEGER NOT NULL DEFAULT 0
);

-- One album folder of a plan. status: eligible (can be tidied), unchanged,
-- blocked (left alone, see reason), stale (its files changed since the
-- check), applied, failed, undone. detail is JSON: where the match came
-- from, the quality, duplicates and the cover.
CREATE TABLE IF NOT EXISTS music_tidy_albums (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id    INTEGER NOT NULL REFERENCES music_tidy_plans(id) ON DELETE CASCADE,
    album_id   INTEGER REFERENCES albums(id) ON DELETE SET NULL, -- NULL for a folder not in the library
    artist     TEXT    NOT NULL DEFAULT '',
    title      TEXT    NOT NULL DEFAULT '',
    old_folder TEXT    NOT NULL,
    new_folder TEXT    NOT NULL DEFAULT '',
    status     TEXT    NOT NULL,
    reason     TEXT    NOT NULL DEFAULT '',
    detail     TEXT    NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_music_tidy_albums_plan ON music_tidy_albums(plan_id);

-- One file of an album: its old and new path, the tags it gets (JSON), and
-- its size and modification time at the check, to notice a change.
CREATE TABLE IF NOT EXISTS music_tidy_items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    album_row  INTEGER NOT NULL REFERENCES music_tidy_albums(id) ON DELETE CASCADE,
    track_id   INTEGER REFERENCES tracks(id) ON DELETE SET NULL,
    old_path   TEXT    NOT NULL,
    new_path   TEXT    NOT NULL,
    plan       TEXT    NOT NULL DEFAULT '{}',
    size       INTEGER NOT NULL DEFAULT 0,
    mtime_ns   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_music_tidy_items_album ON music_tidy_items(album_row);

-- Every step an apply took, in order, so undo can take them back in reverse.
-- step: folder (a folder renamed), mkdir (a folder made), recycle (an
-- original moved to the recycle folder), write (a new copy written), move (a
-- file renamed). size and mtime_ns are of the file at to_path right after the
-- step. undone is 1 once taken back, 2 when it could not be (see note).
CREATE TABLE IF NOT EXISTS music_tidy_journal (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id   INTEGER NOT NULL REFERENCES music_tidy_plans(id) ON DELETE CASCADE,
    album_row INTEGER NOT NULL REFERENCES music_tidy_albums(id) ON DELETE CASCADE,
    step      TEXT    NOT NULL,
    from_path TEXT    NOT NULL DEFAULT '',
    to_path   TEXT    NOT NULL DEFAULT '',
    size      INTEGER NOT NULL DEFAULT 0,
    mtime_ns  INTEGER NOT NULL DEFAULT 0,
    done_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    undone    INTEGER NOT NULL DEFAULT 0,
    note      TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_music_tidy_journal_plan ON music_tidy_journal(plan_id, id);
