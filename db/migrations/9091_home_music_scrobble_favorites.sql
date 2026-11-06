-- Usherr: Last.fm scrobbling, favourites and play history for music
-- (music_lastfm.go, music_favorites.go). Modelled on DroppedNeedle's
-- user_connections, user_favorites and play_history (AGPL-3.0).

-- Each account's own Last.fm connection. session is the session key,
-- encrypted with the server key.
CREATE TABLE IF NOT EXISTS music_lastfm (
    user_id      INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    session      TEXT    NOT NULL,
    lf_user      TEXT    NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL DEFAULT 1,
    love_sync    INTEGER NOT NULL DEFAULT 0, -- a like in Usherr loves the track on Last.fm
    connected_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_error   TEXT    NOT NULL DEFAULT '',
    submitted    INTEGER NOT NULL DEFAULT 0
);

-- Plays Last.fm couldn't take yet (offline, busy), sent with the next one.
CREATE TABLE IF NOT EXISTS music_lastfm_pending (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id    INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    listened_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_music_lastfm_pending_user ON music_lastfm_pending(user_id, listened_at);

-- Liked tracks, albums and artists (kind = track|album|artist), per account.
-- The heart in the player and the star in Subsonic apps.
CREATE TABLE IF NOT EXISTS music_favorites (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL,
    item_id    INTEGER NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (user_id, kind, item_id)
);

-- Every play that counted, from the web player or a Subsonic app.
CREATE TABLE IF NOT EXISTS music_play_history (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id  INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    played_at INTEGER NOT NULL, -- unix seconds the play started
    source    TEXT    NOT NULL DEFAULT 'web'
);
CREATE INDEX IF NOT EXISTS idx_music_play_history_user ON music_play_history(user_id, played_at);
CREATE INDEX IF NOT EXISTS idx_music_play_history_track ON music_play_history(track_id);
