-- Usherr: what's waiting for a person to decide, and what each search found.

CREATE TABLE manga_reviews (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id     INTEGER NOT NULL REFERENCES manga_series(id) ON DELETE CASCADE,
    volume_id     INTEGER NOT NULL DEFAULT 0,        -- 0: a pack, for the series
    kind          TEXT NOT NULL DEFAULT 'single',    -- single, pack, copy (another copy of a volume you have)
    title         TEXT NOT NULL,
    download_url  TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    protocol      TEXT NOT NULL DEFAULT '',
    indexer       TEXT NOT NULL DEFAULT '',
    score         INTEGER NOT NULL DEFAULT 0,
    class         TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '[]',        -- JSON: why it wasn't certain
    added_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (series_id, volume_id, download_url)
);

-- The last search of a volume, or of the series by name (volume_id 0), and when
-- to look again. empties counts searches in a row that found nothing, and sets
-- how long Usherr waits.
CREATE TABLE manga_search_log (
    series_id     INTEGER NOT NULL REFERENCES manga_series(id) ON DELETE CASCADE,
    volume_id     INTEGER NOT NULL DEFAULT 0,
    searched_at   TEXT NOT NULL DEFAULT '',
    outcome       TEXT NOT NULL DEFAULT '',          -- grabbed, review, nothing
    details       TEXT NOT NULL DEFAULT '{}',        -- JSON: manga.SearchSummary
    next_at       TEXT NOT NULL DEFAULT '',
    empties       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (series_id, volume_id)
);
