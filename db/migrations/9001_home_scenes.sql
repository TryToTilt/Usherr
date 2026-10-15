-- Home fork: the Scenes module (adult scenes, administrators only).
-- Named 9001_home_* so it never collides with an upstream migration number;
-- migrations are tracked by name, so upstream ones added later still apply.

CREATE TABLE scene_performers (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    stash_id       TEXT NOT NULL UNIQUE,
    name           TEXT NOT NULL,
    disambiguation TEXT NOT NULL DEFAULT '',
    aliases        TEXT NOT NULL DEFAULT '[]', -- JSON array of names
    image_url      TEXT NOT NULL DEFAULT '',
    followed       INTEGER NOT NULL DEFAULT 0,
    checked_at     TEXT NOT NULL DEFAULT '',   -- when followed scenes were last looked up
    added_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE scene_studios (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    stash_id    TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    aliases     TEXT NOT NULL DEFAULT '[]',
    parent_name TEXT NOT NULL DEFAULT '',
    image_url   TEXT NOT NULL DEFAULT '',
    followed    INTEGER NOT NULL DEFAULT 0,
    checked_at  TEXT NOT NULL DEFAULT '',
    added_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE scenes (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    stash_id        TEXT NOT NULL UNIQUE,
    title           TEXT NOT NULL DEFAULT '',
    studio_stash_id TEXT NOT NULL DEFAULT '',
    studio_name     TEXT NOT NULL DEFAULT '',
    studio_aliases  TEXT NOT NULL DEFAULT '[]',
    release_date    TEXT NOT NULL DEFAULT '', -- YYYY-MM-DD
    duration        INTEGER NOT NULL DEFAULT 0, -- seconds
    code            TEXT NOT NULL DEFAULT '',
    details         TEXT NOT NULL DEFAULT '',
    image_url       TEXT NOT NULL DEFAULT '',
    performers      TEXT NOT NULL DEFAULT '[]', -- JSON [{"id":..,"name":..}]
    wanted          INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'missing', -- missing, downloading, downloaded
    file_path       TEXT NOT NULL DEFAULT '',
    quality         TEXT NOT NULL DEFAULT '',
    added_by        TEXT NOT NULL DEFAULT 'manual', -- manual, performer, studio, import
    added_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX scenes_wanted ON scenes(wanted, status);
CREATE INDEX scenes_studio_date ON scenes(studio_stash_id, release_date);

-- Which download is for which scene. Kept apart from download_queue so the
-- shared queue table needs no fork-only column.
CREATE TABLE scene_downloads (
    queue_id INTEGER PRIMARY KEY REFERENCES download_queue(id) ON DELETE CASCADE,
    scene_id INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE
);
CREATE INDEX scene_downloads_scene ON scene_downloads(scene_id);
