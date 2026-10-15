-- Home fork: the moods of films and shows (feel-good, edge of your seat…)
-- for the Library's "Rows by: Mood" shelves. The AI tags each title once;
-- a title it found no mood for is kept with an empty list, so it isn't asked
-- again.
CREATE TABLE home_title_moods (
    kind       TEXT NOT NULL, -- movie, tv
    tmdb_id    INTEGER NOT NULL,
    moods      TEXT NOT NULL DEFAULT '', -- space separated, from the fixed list
    tagged_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (kind, tmdb_id)
);
