-- Home fork: the family filter on Discover. Each title's youngest suitable
-- age, worked out once: Common Sense Media's age (MDBList), else the US
-- rating, else an AI estimate.
CREATE TABLE home_family_ages (
    kind       TEXT NOT NULL,    -- movie, tv
    tmdb_id    INTEGER NOT NULL,
    age        INTEGER NOT NULL, -- -1: not known
    source     TEXT NOT NULL,    -- csm, rating, ai, none
    rating     TEXT NOT NULL DEFAULT '',
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (kind, tmdb_id)
);
