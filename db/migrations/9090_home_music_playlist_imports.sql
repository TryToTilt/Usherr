-- Usherr: .m3u and .m3u8 playlists brought in from the music folder
-- (music_playlist_import.go). One row per playlist file: the Usherr playlist
-- made from it (NULL once someone deletes that playlist, so the scheduled
-- import doesn't bring it back), what the file looked like when it was read,
-- and the entries that matched no track.
CREATE TABLE IF NOT EXISTS music_playlist_imports (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    path        TEXT    NOT NULL UNIQUE,  -- the playlist file, as the server sees it
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    playlist_id INTEGER REFERENCES music_playlists(id) ON DELETE SET NULL,
    mod_time    INTEGER NOT NULL DEFAULT 0, -- unix seconds
    size        INTEGER NOT NULL DEFAULT 0,
    entries     INTEGER NOT NULL DEFAULT 0,
    matched     INTEGER NOT NULL DEFAULT 0,
    unmatched   TEXT    NOT NULL DEFAULT '[]', -- JSON: [{line, entry}], the first 200
    imported_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_music_playlist_imports_playlist ON music_playlist_imports(playlist_id);
