-- Prefer season packs once a season has ended. A show follows the setting in
-- Settings by default (''), or says 'on' or 'off' for itself.
ALTER TABLE series ADD COLUMN season_pack TEXT NOT NULL DEFAULT '';

-- When Mediarium last looked for a season pack of a season, so the same
-- season is not searched for again and again (once a week at most).
CREATE TABLE IF NOT EXISTS season_pack_searches (
    series_id   INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    season      INTEGER NOT NULL,
    searched_at TEXT    NOT NULL,
    PRIMARY KEY (series_id, season)
);
