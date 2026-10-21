-- Usherr: Real TV remembers the AI's choices. What stands in for a slot that
-- can't be replayed (the 6 PM local news on a channel), and the backup for a
-- show that may not be on Usenet yet, are asked of the AI once and reused on
-- every later build and nightly refresh.
CREATE TABLE tv_replay_picks (
    key         TEXT PRIMARY KEY,           -- call sign | what aired | hour (| backup)
    series_id   INTEGER NOT NULL DEFAULT 0, -- a library show
    stream_tmdb INTEGER NOT NULL DEFAULT 0, -- or a show to stream
    show        TEXT NOT NULL DEFAULT '',
    why         TEXT NOT NULL DEFAULT '',
    updated_ms  INTEGER NOT NULL DEFAULT 0
);
