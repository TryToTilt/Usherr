-- Home fork: what each film and show is made of (genres, keywords, cast,
-- director or creators, year, language), fetched from TMDB once, for the
-- taste engine. A cache: emptying it only means fetching again.
CREATE TABLE home_title_traits (
    kind       TEXT NOT NULL,    -- movie or tv
    tmdb_id    INTEGER NOT NULL,
    data       TEXT NOT NULL,    -- JSON, metadata.TitleTraits; {} when TMDB has no such title
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (kind, tmdb_id)
);
