-- Usherr: a manga series' volumes. "Downloading" and "waiting for you" aren't
-- kept here: they're read from the queue and the review list, so they can't go stale.

CREATE TABLE manga_volumes (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id     INTEGER NOT NULL REFERENCES manga_series(id) ON DELETE CASCADE,
    number        TEXT NOT NULL,                     -- "1", "12.5"
    sort_key      REAL NOT NULL DEFAULT 0,
    title         TEXT NOT NULL DEFAULT '',
    wanted        INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'missing',   -- missing, downloaded
    file_path     TEXT NOT NULL DEFAULT '',
    file_size     INTEGER NOT NULL DEFAULT 0,
    release_size  INTEGER NOT NULL DEFAULT 0,          -- as it came, before ComicInfo.xml went in: tells a second copy apart
    source_class  TEXT NOT NULL DEFAULT '',          -- official, fan, unknown, own
    added_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (series_id, number)
);
CREATE INDEX manga_volumes_wanted ON manga_volumes(wanted, status);
