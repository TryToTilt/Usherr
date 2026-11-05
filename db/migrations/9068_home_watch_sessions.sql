-- Usherr: watch history (internal/watchlog). Usherr asks Jellyfin, Emby and
-- Plex what is playing every half minute and keeps one row per playback: who,
-- what, on which app and device, how it played (direct or transcoded, and
-- why), how long it was actually watched (pauses left out) and how far it got.
-- It does the job of a separate Streamystats or Tautulli.
--
-- user_id is the Usherr account when the server's user could be matched, 0
-- otherwise. library_id is the Usherr movie's or show's id (kind says which),
-- tmdb_id the film's or the show's. location is 'lan', 'remote' or ''; address
-- is kept only while "Keep players' addresses" is on, and can be cleared.
-- ended_ms is 0 while it is still playing.
CREATE TABLE watch_sessions (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id         INTEGER NOT NULL DEFAULT 0,
    server_name       TEXT    NOT NULL DEFAULT '',
    server_kind       TEXT    NOT NULL DEFAULT '',
    session_key       TEXT    NOT NULL DEFAULT '',
    server_user_id    TEXT    NOT NULL DEFAULT '',
    user_name         TEXT    NOT NULL DEFAULT '',
    user_id           INTEGER NOT NULL DEFAULT 0,
    kind              TEXT    NOT NULL DEFAULT 'other',
    title             TEXT    NOT NULL DEFAULT '',
    series            TEXT    NOT NULL DEFAULT '',
    season            INTEGER NOT NULL DEFAULT 0,
    episode           INTEGER NOT NULL DEFAULT 0,
    year              INTEGER NOT NULL DEFAULT 0,
    tmdb_id           INTEGER NOT NULL DEFAULT 0,
    library_id        INTEGER NOT NULL DEFAULT 0,
    client            TEXT    NOT NULL DEFAULT '',
    device            TEXT    NOT NULL DEFAULT '',
    play_method       TEXT    NOT NULL DEFAULT '',
    transcode_reasons TEXT    NOT NULL DEFAULT '',
    video_in          TEXT    NOT NULL DEFAULT '',
    video_out         TEXT    NOT NULL DEFAULT '',
    audio_in          TEXT    NOT NULL DEFAULT '',
    audio_out         TEXT    NOT NULL DEFAULT '',
    resolution_in     TEXT    NOT NULL DEFAULT '',
    resolution_out    TEXT    NOT NULL DEFAULT '',
    bandwidth_kbps    INTEGER NOT NULL DEFAULT 0,
    location          TEXT    NOT NULL DEFAULT '',
    address           TEXT    NOT NULL DEFAULT '',
    started_ms        INTEGER NOT NULL DEFAULT 0,
    ended_ms          INTEGER NOT NULL DEFAULT 0,
    last_seen_ms      INTEGER NOT NULL DEFAULT 0,
    watched_sec       INTEGER NOT NULL DEFAULT 0,
    progress          REAL    NOT NULL DEFAULT 0,
    runtime_sec       INTEGER NOT NULL DEFAULT 0,
    paused            INTEGER NOT NULL DEFAULT 0,
    completed         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX watch_sessions_by_start ON watch_sessions(started_ms);
CREATE INDEX watch_sessions_by_user ON watch_sessions(user_id, started_ms);
CREATE INDEX watch_sessions_by_title ON watch_sessions(kind, library_id);
CREATE INDEX watch_sessions_open ON watch_sessions(ended_ms);
