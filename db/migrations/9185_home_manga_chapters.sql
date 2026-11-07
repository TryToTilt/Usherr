-- Usherr: chapters, for series followed through MangaDex. "Downloading" and "In
-- Volume N" aren't kept here: they're read from the queue and from the volumes,
-- so they can't go stale. One row a chapter number; manga_chapter_copies keeps
-- every group's upload of it, and dex_id on the row is the one chosen.

CREATE TABLE manga_chapters (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id     INTEGER NOT NULL REFERENCES manga_series(id) ON DELETE CASCADE,
    number        TEXT NOT NULL,                     -- "12", "12.5", or "Oneshot"
    sort_key      REAL NOT NULL DEFAULT 0,
    volume        TEXT NOT NULL DEFAULT '',          -- the volume MangaDex files it under, normalised
    title         TEXT NOT NULL DEFAULT '',
    group_key     TEXT NOT NULL DEFAULT '',          -- the chosen copy's group ids
    group_name    TEXT NOT NULL DEFAULT '',          -- who translated it, as credited
    language      TEXT NOT NULL DEFAULT 'en',
    source        TEXT NOT NULL DEFAULT 'mangadex',  -- mangadex, or file for a pack or a file already there
    dex_id        TEXT NOT NULL DEFAULT '',          -- the chosen copy on MangaDex
    pages         INTEGER NOT NULL DEFAULT 0,
    external_url  TEXT NOT NULL DEFAULT '',          -- hosted elsewhere: can't be downloaded
    unavailable   INTEGER NOT NULL DEFAULT 0,        -- listed, with its pages taken down
    published_at  TEXT NOT NULL DEFAULT '',
    wanted        INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'missing',   -- missing, downloaded
    file_path     TEXT NOT NULL DEFAULT '',
    file_size     INTEGER NOT NULL DEFAULT 0,
    set_aside     INTEGER NOT NULL DEFAULT 0,        -- "Tidy chapters" moved the file to .replaced
    queue_id      INTEGER NOT NULL DEFAULT 0,        -- the batch it last belonged to
    tries         INTEGER NOT NULL DEFAULT 0,
    next_try      TEXT NOT NULL DEFAULT '',
    problem       TEXT NOT NULL DEFAULT '',
    added_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (series_id, number)
);
CREATE INDEX manga_chapters_wanted ON manga_chapters(wanted, status);
CREATE INDEX manga_chapters_queue ON manga_chapters(queue_id);

CREATE TABLE manga_chapter_copies (
    chapter_id   INTEGER NOT NULL REFERENCES manga_chapters(id) ON DELETE CASCADE,
    dex_id       TEXT NOT NULL,
    group_key    TEXT NOT NULL DEFAULT '',
    group_name   TEXT NOT NULL DEFAULT '',
    pages        INTEGER NOT NULL DEFAULT 0,
    external_url TEXT NOT NULL DEFAULT '',
    unavailable  INTEGER NOT NULL DEFAULT 0,
    published_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (chapter_id, dex_id)
);

-- A series' link to MangaDex, and what the last look found.
CREATE TABLE manga_dex_series (
    series_id    INTEGER PRIMARY KEY REFERENCES manga_series(id) ON DELETE CASCADE,
    dex_id       TEXT NOT NULL DEFAULT '',
    link         TEXT NOT NULL DEFAULT '',           -- '', linked, unsure, none
    link_how     TEXT NOT NULL DEFAULT '',           -- anilist, mangadex, title, person
    candidates   TEXT NOT NULL DEFAULT '[]',         -- for a person to choose from, when unsure
    follow       TEXT NOT NULL DEFAULT '',           -- '' off, new, all
    prefer_group TEXT NOT NULL DEFAULT '',           -- a group_key chosen for this series
    checked_at   TEXT NOT NULL DEFAULT '',
    next_at      TEXT NOT NULL DEFAULT '',
    empties      INTEGER NOT NULL DEFAULT 0,
    trouble      TEXT NOT NULL DEFAULT '',           -- the last look failed: down, busy, refused
    stats        TEXT NOT NULL DEFAULT '{}'
);

-- Who has finished which chapter. Where someone is in a chapter stays in
-- home_positions, as item "manga:c:<chapter id>".
CREATE TABLE manga_chapter_read (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chapter_id INTEGER NOT NULL REFERENCES manga_chapters(id) ON DELETE CASCADE,
    read_ms    INTEGER NOT NULL,
    PRIMARY KEY (user_id, chapter_id)
);
CREATE INDEX manga_chapter_read_newest ON manga_chapter_read (user_id, read_ms);

-- A download is a release from an indexer, or a batch of chapters from MangaDex.
ALTER TABLE manga_downloads ADD COLUMN source TEXT NOT NULL DEFAULT '';
