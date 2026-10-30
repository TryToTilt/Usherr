-- Usherr: artwork picking (home_artwork.go), after Posterizarr. One row per
-- title Jellyfin has (a film, a show or a collection, by its TMDB id), with
-- the Jellyfin items that are it (the library and the streamed copy) and
-- what it lacks; and each change: proposed, made (with the picture it
-- replaced, so it can be put back), skipped or undone.
CREATE TABLE art_titles (
    kind       TEXT NOT NULL,              -- movie, tv or collection
    tmdb_id    INTEGER NOT NULL,
    tvdb_id    INTEGER NOT NULL DEFAULT 0,
    title      TEXT NOT NULL DEFAULT '',
    year       INTEGER NOT NULL DEFAULT 0,
    jf_ids     TEXT NOT NULL DEFAULT '',   -- Jellyfin item ids, comma separated
    missing    TEXT NOT NULL DEFAULT '',   -- poster,backdrop,logo
    checked_ms INTEGER NOT NULL DEFAULT 0, -- last looked at for art
    error      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (kind, tmdb_id)
);

CREATE TABLE art_changes (
    id         INTEGER PRIMARY KEY,
    kind       TEXT NOT NULL,
    tmdb_id    INTEGER NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    type       TEXT NOT NULL,               -- poster, backdrop or logo
    source     TEXT NOT NULL DEFAULT '',    -- tmdb or fanart
    url        TEXT NOT NULL,               -- the full picture
    preview    TEXT NOT NULL DEFAULT '',    -- a small one, to choose by
    width      INTEGER NOT NULL DEFAULT 0,
    height     INTEGER NOT NULL DEFAULT 0,
    lang       TEXT NOT NULL DEFAULT '',
    why        TEXT NOT NULL DEFAULT '',    -- missing, small or chosen
    state      TEXT NOT NULL DEFAULT 'waiting', -- waiting, done, skipped, undone or failed
    error      TEXT NOT NULL DEFAULT '',
    backups    TEXT NOT NULL DEFAULT '',    -- JSON: Jellyfin item id -> the file of the picture replaced
    by_hand    INTEGER NOT NULL DEFAULT 0,  -- picked by a person: kept, never replaced by a pass
    created_ms INTEGER NOT NULL,
    done_ms    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX art_changes_title ON art_changes (kind, tmdb_id, type);
CREATE INDEX art_changes_state ON art_changes (state);
