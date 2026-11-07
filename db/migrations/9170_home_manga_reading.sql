-- Usherr: reading manga, per person. Where someone is in a volume stays in
-- home_positions (item "manga:v:<volume id>"), shared with the other readers; this
-- adds what they finished, and how they like to read each series.
CREATE TABLE manga_read (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    volume_id INTEGER NOT NULL REFERENCES manga_volumes(id) ON DELETE CASCADE,
    read_ms   INTEGER NOT NULL,
    PRIMARY KEY (user_id, volume_id)
);
CREATE INDEX manga_read_newest ON manga_read (user_id, read_ms);

-- direction: ltr or rtl; layout: single, double or webtoon. Empty means not
-- chosen, so the series' format and the file decide.
CREATE TABLE manga_reader_prefs (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    series_id INTEGER NOT NULL REFERENCES manga_series(id) ON DELETE CASCADE,
    direction TEXT NOT NULL DEFAULT '',
    layout    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, series_id)
);
