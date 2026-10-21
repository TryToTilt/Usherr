-- Usherr: Showtimes at home. A film booked for a time: the Marquee counts
-- down to it, the pre-show starts on time, and the film starts where it was
-- booked to play (a Showtime screen in a browser, or the TV).
CREATE TABLE showtimes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL,
    tmdb_id     INTEGER NOT NULL,
    title       TEXT    NOT NULL,
    year        INTEGER NOT NULL DEFAULT 0,
    poster_url  TEXT    NOT NULL DEFAULT '',
    runtime     INTEGER NOT NULL DEFAULT 0,   -- minutes; 0 when unknown
    at_ms       INTEGER NOT NULL,             -- when the film starts
    preshow_min INTEGER NOT NULL DEFAULT 10,  -- minutes of pre-show before it; 0 for none
    play_on     TEXT    NOT NULL DEFAULT 'here', -- here (a Showtime screen), tv (Play on TV) or none
    state       TEXT    NOT NULL DEFAULT 'booked', -- booked, preshow, playing, done
    created_ms  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX showtimes_at ON showtimes (at_ms);
