-- Home fork: the people (actors, directors, writers) each person follows, for
-- the "From people you follow" Discover rows and the Following tab.
CREATE TABLE home_follows (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    person_id  INTEGER NOT NULL, -- TMDB person id
    name       TEXT NOT NULL DEFAULT '',
    image      TEXT NOT NULL DEFAULT '',
    known      TEXT NOT NULL DEFAULT '', -- Acting, Directing, Writing
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (user_id, person_id)
);
