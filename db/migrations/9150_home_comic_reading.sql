-- Usherr: reading comics, per person (internal/comics/home_reading.go).
-- comic_read: issues someone finished or marked read. home_positions forgets an
-- issue read to the end, so "read" lives here; both are keyed to the issue's
-- row, so a rename, a convert or a fixed ComicVine match keeps them.
CREATE TABLE comic_read (
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issue_id INTEGER NOT NULL REFERENCES comic_issues(id) ON DELETE CASCADE,
    read_ms  INTEGER NOT NULL,
    PRIMARY KEY (user_id, issue_id)
);
CREATE INDEX comic_read_newest ON comic_read (user_id, read_ms);

-- Want to Read: a whole series (issue_id 0) or one issue.
CREATE TABLE comic_want_read (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    series_id INTEGER NOT NULL REFERENCES comic_series(id) ON DELETE CASCADE,
    issue_id  INTEGER NOT NULL DEFAULT 0,
    added_ms  INTEGER NOT NULL,
    PRIMARY KEY (user_id, series_id, issue_id)
);

-- How someone reads: for one series, or their default (series_id 0).
-- direction: ltr or rtl; layout: flip, slide, double, webtoon or panels; empty
-- means not chosen.
CREATE TABLE comic_reader_prefs (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    series_id INTEGER NOT NULL DEFAULT 0,
    direction TEXT NOT NULL DEFAULT '',
    layout    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, series_id)
);

-- What kind of series it is, set by hand when the publisher doesn't tell:
-- comic, manga, webtoon or collected. Empty: not set.
ALTER TABLE comic_series ADD COLUMN series_type TEXT NOT NULL DEFAULT '';

-- Which way a page's panels were put in order: 1 right to left, 0 left to
-- right, -1 from before this column (by the series' publisher).
ALTER TABLE comic_panels ADD COLUMN rtl INTEGER NOT NULL DEFAULT -1;
