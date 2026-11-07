-- Usherr: what a downloaded manga release turned out to hold, when that's why it
-- couldn't be filed (only a novel, only MOBI files). It isn't a bad release, so it's
-- not blocklisted, but it isn't downloaded again without a person asking.
CREATE TABLE manga_release_notes (
    series_id  INTEGER NOT NULL REFERENCES manga_series(id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    note       TEXT NOT NULL,
    PRIMARY KEY (series_id, title)
);
