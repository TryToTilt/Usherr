-- Usherr: each person's Stack, the films and shows they pulled out to watch
-- next (in the sidebar, under Settings). Top of the stack first.
CREATE TABLE user_stack (
    user_id    INTEGER NOT NULL,
    kind       TEXT NOT NULL,                 -- 'movie' or 'tv'
    tmdb_id    INTEGER NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    year       INTEGER NOT NULL DEFAULT 0,
    poster_url TEXT NOT NULL DEFAULT '',
    position   INTEGER NOT NULL DEFAULT 0,    -- lowest is the top
    added_ms   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, kind, tmdb_id)
);
