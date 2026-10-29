-- Usherr: where each person got to in a video or a comic, so it picks up
-- there next time (home_positions.go). item names what it is: movie:<id>,
-- episode:<id>, video:<id>, title:movie:<tmdb>, title:tv:<tmdb>:<s>:<e>,
-- sport:<id> or comic:<issue>. position and duration are seconds, or pages
-- for a comic. Finished (or barely started) things have no row. Ebooks and
-- audiobooks keep theirs in book_progress.
CREATE TABLE home_positions (
    user_id    INTEGER NOT NULL,
    item       TEXT NOT NULL,
    position   REAL NOT NULL,
    duration   REAL NOT NULL DEFAULT 0,
    updated_ms INTEGER NOT NULL,
    PRIMARY KEY (user_id, item)
);
