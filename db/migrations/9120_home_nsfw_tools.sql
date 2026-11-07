-- Usherr: NSFW 18+ tools (internal/nsfw/identify.go, rename.go): matching files
-- by fingerprint, and renaming with a preview and undo.

-- The last "Find matches" run: one file each. status: matched, none,
-- unreadable, registered, blocked. video_id is set for a video already in the
-- library that has no StashDB id. candidates is JSON, best first.
CREATE TABLE IF NOT EXISTS nsfw_identify_items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    path       TEXT    NOT NULL UNIQUE,
    size       INTEGER NOT NULL DEFAULT 0,
    mtime_ns   INTEGER NOT NULL DEFAULT 0,
    video_id   INTEGER REFERENCES nsfw_videos(id) ON DELETE SET NULL,
    oshash     TEXT    NOT NULL DEFAULT '',
    duration   INTEGER NOT NULL DEFAULT 0,
    quality    TEXT    NOT NULL DEFAULT '',
    status     TEXT    NOT NULL,
    reason     TEXT    NOT NULL DEFAULT '',
    candidates TEXT    NOT NULL DEFAULT '[]',
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- One "Check names". status: preview, applied (some of it was renamed), undone.
CREATE TABLE IF NOT EXISTS nsfw_rename_plans (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    root       TEXT    NOT NULL,
    status     TEXT    NOT NULL DEFAULT 'preview',
    unchanged  INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    applied_at TEXT    NOT NULL DEFAULT '',
    undone_at  TEXT    NOT NULL DEFAULT ''
);

-- One video of a plan. status: eligible, blocked (left alone, see reason),
-- stale (changed since the check), applying (stays so after a crash halfway),
-- applied, failed, undone. sidecars is JSON: the subtitle and .nfo files that
-- move along. outside is 1 for a file that isn't in the NSFW folder.
CREATE TABLE IF NOT EXISTS nsfw_rename_items (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id  INTEGER NOT NULL REFERENCES nsfw_rename_plans(id) ON DELETE CASCADE,
    video_id INTEGER REFERENCES nsfw_videos(id) ON DELETE SET NULL,
    name     TEXT    NOT NULL DEFAULT '',
    old_path TEXT    NOT NULL,
    new_path TEXT    NOT NULL DEFAULT '',
    size     INTEGER NOT NULL DEFAULT 0,
    mtime_ns INTEGER NOT NULL DEFAULT 0,
    sidecars TEXT    NOT NULL DEFAULT '[]',
    outside  INTEGER NOT NULL DEFAULT 0,
    status   TEXT    NOT NULL,
    reason   TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_nsfw_rename_items_plan ON nsfw_rename_items(plan_id);

-- Every step an apply took, written before the step is taken, so a crash
-- halfway can still be undone. step: mkdir (a folder made), move (a file
-- renamed). primary_file is 1 for the video itself. state: 0 about to happen,
-- 1 done, 2 taken back, 3 could not be taken back (see note).
CREATE TABLE IF NOT EXISTS nsfw_rename_journal (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id      INTEGER NOT NULL REFERENCES nsfw_rename_plans(id) ON DELETE CASCADE,
    item_id      INTEGER NOT NULL REFERENCES nsfw_rename_items(id) ON DELETE CASCADE,
    step         TEXT    NOT NULL,
    primary_file INTEGER NOT NULL DEFAULT 0,
    from_path    TEXT    NOT NULL DEFAULT '',
    to_path      TEXT    NOT NULL DEFAULT '',
    size         INTEGER NOT NULL DEFAULT 0,
    mtime_ns     INTEGER NOT NULL DEFAULT 0,
    state        INTEGER NOT NULL DEFAULT 0,
    note         TEXT    NOT NULL DEFAULT '',
    done_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_nsfw_rename_journal_plan ON nsfw_rename_journal(plan_id, id);
