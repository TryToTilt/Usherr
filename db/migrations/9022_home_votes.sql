-- Home fork: each person's thumbs up or down, for films, shows, music, books
-- and comics, in the library or not. Private to the person; a thumbs down
-- hides the title from that person's Discover.
CREATE TABLE home_votes (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,    -- movie, tv, artist, album, book, comic
    ref        TEXT NOT NULL,    -- TMDB id (movie, tv), MusicBrainz id, book work key, ComicVine id
    vote       INTEGER NOT NULL, -- 1 or -1
    title      TEXT NOT NULL DEFAULT '',
    year       INTEGER NOT NULL DEFAULT 0,
    image      TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (user_id, kind, ref)
);
CREATE INDEX home_votes_recent ON home_votes (user_id, updated_at);
