-- Usherr: coming-soon trailers kept for the pre-show (home_preshow_trailers.go).
-- A title comes in when its release is near (a wanted film, one coming to
-- cinemas, a show's new season); its trailer is downloaded ahead so it plays
-- at once, in the Jellyfin app too, and goes again once the title is out.
CREATE TABLE preshow_trailers (
    id           INTEGER PRIMARY KEY,
    kind         TEXT NOT NULL,              -- movie or tv
    tmdb_id      INTEGER NOT NULL,
    season       INTEGER NOT NULL DEFAULT 0, -- tv: the new season
    title        TEXT NOT NULL,
    release_date TEXT NOT NULL DEFAULT '',   -- YYYY-MM-DD: in cinemas, or the season's first episode
    why          TEXT NOT NULL DEFAULT '',   -- wanted, cinema or season
    poster_url   TEXT NOT NULL DEFAULT '',
    youtube      TEXT NOT NULL DEFAULT '',
    file         TEXT NOT NULL DEFAULT '',   -- in the trailers folder
    size         INTEGER NOT NULL DEFAULT 0,
    seconds      REAL NOT NULL DEFAULT 0,
    state        TEXT NOT NULL DEFAULT 'waiting', -- waiting, ready or failed
    error        TEXT NOT NULL DEFAULT '',
    tries        INTEGER NOT NULL DEFAULT 0,
    keep         INTEGER NOT NULL DEFAULT 0, -- kept past its release and the cap
    added_ms     INTEGER NOT NULL,
    fetched_ms   INTEGER NOT NULL DEFAULT 0,
    UNIQUE (kind, tmdb_id, season)
);
