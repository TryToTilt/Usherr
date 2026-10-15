-- Home fork: which film series (TMDB collection) each film the household has
-- belongs to (0: none), for Discover's "Complete the collection".
CREATE TABLE home_movie_collections (
    tmdb_id       INTEGER PRIMARY KEY,
    collection_id INTEGER NOT NULL,
    checked_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
