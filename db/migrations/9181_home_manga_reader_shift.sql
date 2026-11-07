-- Usherr: two pages at a time, whether a series' pairs start one page later, so its
-- two-page spreads line up. 1 shifted, -1 not, 0 not chosen.
ALTER TABLE manga_reader_prefs ADD COLUMN shift INTEGER NOT NULL DEFAULT 0;
