-- Home fork: Streams for you. A person who switches it on gets the films that
-- fit their taste best added to the streamed library by themselves (a few a
-- week, very strong matches only). Nothing is downloaded and nothing is ever
-- removed by itself; this remembers what was added, for whom.
CREATE TABLE home_autostream (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE home_autostream_added (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,       -- movie
    tmdb_id     INTEGER NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    year        INTEGER NOT NULL DEFAULT 0,
    image       TEXT NOT NULL DEFAULT '',
    match       INTEGER NOT NULL DEFAULT 0,
    because     TEXT NOT NULL DEFAULT '', -- the liked or watched title it's most like
    streamed_id INTEGER NOT NULL DEFAULT 0,
    added_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    removed_at  TEXT NOT NULL DEFAULT ''  -- taken out by the person
);
CREATE INDEX home_autostream_added_user ON home_autostream_added (user_id, added_at);
