-- Usherr: MangaDex chapters read online, without downloading (home_manga_browse.go).
-- Where someone is in one stays in home_positions, as item "manga:d:<chapter id>";
-- this keeps what the Continue row needs to show it: the series, the number and
-- the cover, as they were when it was opened.

CREATE TABLE manga_dex_reads (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chapter_id  TEXT NOT NULL,                     -- MangaDex's chapter id
    manga_id    TEXT NOT NULL,                     -- MangaDex's series id
    title       TEXT NOT NULL DEFAULT '',
    number      TEXT NOT NULL DEFAULT '',
    cover_file  TEXT NOT NULL DEFAULT '',
    adult       INTEGER NOT NULL DEFAULT 0,        -- erotica or pornographic
    opened_ms   INTEGER NOT NULL,
    PRIMARY KEY (user_id, chapter_id)
);
CREATE INDEX manga_dex_reads_newest ON manga_dex_reads (user_id, opened_ms);
