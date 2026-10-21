-- The cinema and digital release dates of a movie in the person's country, for
-- the calendar. TMDB gives them in the detail call of each title, so they are
-- fetched slowly in the background and kept here. checked_at is when TMDB was
-- last asked; region is the country the dates were read for, so a changed
-- info language asks again.
CREATE TABLE IF NOT EXISTS movie_release_dates (
    tmdb_id    INTEGER NOT NULL PRIMARY KEY,
    region     TEXT    NOT NULL DEFAULT '',
    cinema     TEXT    NOT NULL DEFAULT '',  -- 'YYYY-MM-DD' or empty
    digital    TEXT    NOT NULL DEFAULT '',
    checked_at TEXT    NOT NULL DEFAULT ''
);
