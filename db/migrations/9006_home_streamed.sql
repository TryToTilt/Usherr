-- Home fork: the streamed library. Movies and shows Mediarium plays through
-- StreamNZB instead of keeping their files: each has a saved NZB (the exact
-- release), and Jellyfin sees it through a small .strm pointer file. Kept
-- apart from movies/series so nothing here is ever searched for or
-- downloaded unless someone asks.

CREATE TABLE streamed_titles (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    kind          TEXT NOT NULL,                 -- movie | series
    tmdb_id       INTEGER NOT NULL,
    imdb_id       TEXT NOT NULL DEFAULT '',
    tvdb_id       INTEGER NOT NULL DEFAULT 0,
    title         TEXT NOT NULL,
    year          INTEGER NOT NULL DEFAULT 0,
    overview      TEXT NOT NULL DEFAULT '',
    poster_path   TEXT NOT NULL DEFAULT '',
    backdrop_path TEXT NOT NULL DEFAULT '',
    genres        TEXT NOT NULL DEFAULT '[]',
    rating        TEXT NOT NULL DEFAULT '',      -- age rating: PG-13, TV-14
    score         REAL NOT NULL DEFAULT 0,       -- TMDB's, out of 10
    anime         INTEGER NOT NULL DEFAULT 0,
    added_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (kind, tmdb_id)
);

-- One playable file: a movie, or one episode of a show.
CREATE TABLE streamed_files (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title_id   INTEGER NOT NULL REFERENCES streamed_titles(id) ON DELETE CASCADE,
    season     INTEGER NOT NULL DEFAULT 0,
    episode    INTEGER NOT NULL DEFAULT 0,
    name       TEXT NOT NULL DEFAULT '',      -- the episode's title
    overview   TEXT NOT NULL DEFAULT '',
    air_date   TEXT NOT NULL DEFAULT '',
    release    TEXT NOT NULL DEFAULT '',      -- the release name
    nzb        TEXT NOT NULL DEFAULT '',      -- saved NZB, under the config folder's streamed/nzb
    inner_file TEXT NOT NULL DEFAULT '',      -- the video's name inside the release
    size       INTEGER NOT NULL DEFAULT 0,
    strm_path  TEXT NOT NULL DEFAULT '',      -- the pointer file, relative to the streamed folder
    health     TEXT NOT NULL DEFAULT '',      -- '' (unchecked) | ok | dead
    health_at  TEXT NOT NULL DEFAULT '',
    UNIQUE (title_id, season, episode)
);
CREATE INDEX streamed_files_title ON streamed_files (title_id);
