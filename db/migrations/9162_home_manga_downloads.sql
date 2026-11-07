-- Usherr: which download is for which manga volume, beside the usual queue.
-- volume_id is 0 for a pack, which fills whatever volumes it holds.

CREATE TABLE manga_downloads (
    queue_id      INTEGER PRIMARY KEY REFERENCES download_queue(id) ON DELETE CASCADE,
    series_id     INTEGER NOT NULL REFERENCES manga_series(id) ON DELETE CASCADE,
    volume_id     INTEGER NOT NULL DEFAULT 0,
    class         TEXT NOT NULL DEFAULT '',          -- what the release's name said it is
    replace_file  INTEGER NOT NULL DEFAULT 0         -- a person chose this copy over the file there
);
CREATE INDEX manga_downloads_series ON manga_downloads(series_id);
CREATE INDEX manga_downloads_volume ON manga_downloads(volume_id);
