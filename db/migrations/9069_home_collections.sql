-- Usherr: collections pushed to Jellyfin and Plex, and badges on posters
-- (home_collections.go, home_badges.go): what Kometa did.
--
-- A collection: its name, where its titles come from (source: list, an
-- Usherr import list; tmdb, a TMDB collection; trending, TMDB's trending;
-- rule, the library's titles that match; picked, chosen by hand) and the
-- source's details (config, JSON), which servers get it, how it sorts, its
-- poster ('' the server's own, upload, made in the Re-Posterr style), where
-- Plex shows it, whether missing titles are added to Usherr, and whether
-- titles that no longer match are taken out.
CREATE TABLE server_collections (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL,
    source         TEXT    NOT NULL,
    config         TEXT    NOT NULL DEFAULT '{}',
    media          TEXT    NOT NULL DEFAULT 'movie', -- movie, tv or both
    servers        TEXT    NOT NULL DEFAULT 'jellyfin,plex',
    sort_title     TEXT    NOT NULL DEFAULT '',
    poster         TEXT    NOT NULL DEFAULT '',      -- '', upload or made
    visibility     TEXT    NOT NULL DEFAULT '',      -- Plex: library,home,shared
    only_library   INTEGER NOT NULL DEFAULT 1,
    remove_missing INTEGER NOT NULL DEFAULT 1,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_ms     INTEGER NOT NULL DEFAULT 0,
    last_sync_ms   INTEGER NOT NULL DEFAULT 0,
    last_status    TEXT    NOT NULL DEFAULT '',      -- ok or error
    last_message   TEXT    NOT NULL DEFAULT '',
    last_found     INTEGER NOT NULL DEFAULT 0,       -- titles the source names
    last_matched   INTEGER NOT NULL DEFAULT 0,       -- of those, in the library
    last_missing   TEXT    NOT NULL DEFAULT '[]'     -- JSON: a few of those not in the library
);

-- Where each collection is on the servers: target is "jellyfin:<server>" or
-- "plex:<server>:<library>", remote the collection's id there. What was
-- last sent (sort title, poster fingerprint, Plex's visibility) so it isn't
-- sent again.
CREATE TABLE server_collection_links (
    collection_id INTEGER NOT NULL REFERENCES server_collections (id) ON DELETE CASCADE,
    target        TEXT    NOT NULL,
    remote_id     TEXT    NOT NULL,
    sort_sent     TEXT    NOT NULL DEFAULT '',
    poster_sent   TEXT    NOT NULL DEFAULT '',
    vis_sent      TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (collection_id, target)
);

-- Poster badges: one row per copy of a title (Jellyfin's item id, or Re-Posterr's
-- "plex:..." id). base: the original poster's file in the artwork folder's
-- badges/originals; drawn: the fingerprint of the badges the server shows
-- now ('' the original); tag: the server's poster tag after Usherr's upload
-- (another tag means the poster was changed elsewhere). Plex's tracks are
-- read once per file (file_print) and kept in media.
CREATE TABLE badge_posters (
    copy_id    TEXT    PRIMARY KEY,
    title      TEXT    NOT NULL DEFAULT '',
    base       TEXT    NOT NULL DEFAULT '',
    drawn      TEXT    NOT NULL DEFAULT '',
    tag        TEXT    NOT NULL DEFAULT '',
    file_print TEXT    NOT NULL DEFAULT '',
    media      TEXT    NOT NULL DEFAULT '',
    error      TEXT    NOT NULL DEFAULT '',
    updated_ms INTEGER NOT NULL DEFAULT 0
);
