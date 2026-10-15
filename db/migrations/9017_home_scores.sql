-- Home fork: critic and audience scores from MDBList (IMDb, Rotten Tomatoes,
-- Metacritic, Letterboxd, Trakt, Common Sense age), kept a month per title.
CREATE TABLE home_scores (
    kind       TEXT NOT NULL,    -- movie, tv
    tmdb_id    INTEGER NOT NULL,
    data       TEXT NOT NULL DEFAULT '{}', -- titleScores as JSON; {"none":true} when MDBList has nothing
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (kind, tmdb_id)
);
