-- Home fork: Mediarium TV. Scheduled channels built from the library (and,
-- later, free live streams and titles played through StreamNZB), watched in
-- Mediarium's player or through Jellyfin Live TV.

CREATE TABLE tv_channels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    number     INTEGER NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    logo_url   TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL DEFAULT 'library',  -- library | live | stream
    rules      TEXT NOT NULL DEFAULT '{}',        -- JSON: what plays and in which order
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- The schedule: one row per program, back to back.
CREATE TABLE tv_programs (
    channel_id INTEGER NOT NULL REFERENCES tv_channels(id) ON DELETE CASCADE,
    start_ms   INTEGER NOT NULL,
    end_ms     INTEGER NOT NULL,
    kind       TEXT NOT NULL,                     -- episode | movie
    item_id    INTEGER NOT NULL,                  -- episodes.id or movies.id
    series_id  INTEGER NOT NULL DEFAULT 0,
    title      TEXT NOT NULL,
    subtitle   TEXT NOT NULL DEFAULT '',          -- "S02E03 · Episode title"
    file_path  TEXT NOT NULL,
    PRIMARY KEY (channel_id, start_ms)
);
CREATE INDEX tv_programs_end ON tv_programs (channel_id, end_ms);

-- Where each channel is in its rotation (per show: the last episode played).
CREATE TABLE tv_state (
    channel_id INTEGER PRIMARY KEY REFERENCES tv_channels(id) ON DELETE CASCADE,
    state      TEXT NOT NULL DEFAULT '{}'
);

-- How long each file runs, measured once with ffprobe.
CREATE TABLE tv_durations (
    file_path TEXT PRIMARY KEY,
    size      INTEGER NOT NULL,
    seconds   REAL NOT NULL
);
