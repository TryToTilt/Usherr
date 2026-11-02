-- Usherr: Community, ticket stubs. One for each watch party someone went to,
-- like a cinema's: everyone who pressed I'm in, and the host, gets one "at the
-- door" when the film starts, and has it stamped on the way out (a button at
-- the credits, open for half an hour). A stub keeps its own copy of the film
-- and the night, so it stays after the Showtime is gone.
CREATE TABLE community_stubs (
    backend     TEXT    NOT NULL DEFAULT 'discord',
    external_id TEXT    NOT NULL,             -- the person's Discord id
    showtime_id INTEGER NOT NULL,             -- the party (no reference: the stub outlives it)
    name        TEXT    NOT NULL DEFAULT '',
    kind        TEXT    NOT NULL DEFAULT 'movie',
    tmdb_id     INTEGER NOT NULL,
    title       TEXT    NOT NULL,
    year        INTEGER NOT NULL DEFAULT 0,
    poster      TEXT    NOT NULL DEFAULT '',
    host        TEXT    NOT NULL DEFAULT '',
    seat        INTEGER NOT NULL DEFAULT 0,  -- 0: the host
    guests      TEXT    NOT NULL DEFAULT '', -- who else was there, one per line
    at_ms       INTEGER NOT NULL,            -- when the film started
    stamp_until INTEGER NOT NULL DEFAULT 0,  -- the stamp button works until then (0: not yet offered)
    stamped_ms  INTEGER NOT NULL DEFAULT 0,  -- when it was stamped (0: not)
    PRIMARY KEY (backend, external_id, showtime_id)
);
CREATE INDEX community_stubs_person ON community_stubs (external_id, at_ms);
