-- Usherr: each person's own MangaDex account, optional and off until they connect
-- it. Follows, read marks, reading status and lists sync both ways. Everything a
-- person saved here goes when they disconnect. See docs/manga.md.

-- The sign-in. Every one of these values is encrypted with the server key, like
-- indexer passwords, and none is ever sent back to the page or logged. The
-- access token lives in memory only: it lasts 15 minutes.
CREATE TABLE manga_md_accounts (
    user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    client_id     TEXT NOT NULL,
    client_secret TEXT NOT NULL,
    username      TEXT NOT NULL,
    password      TEXT NOT NULL,
    refresh_token TEXT NOT NULL DEFAULT '',
    state         TEXT NOT NULL DEFAULT '',          -- '' not tried yet, ok, refused
    problem       TEXT NOT NULL DEFAULT '',          -- what the page says when something is wrong
    connected_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    light_at      TEXT NOT NULL DEFAULT '',          -- the last small sync
    full_at       TEXT NOT NULL DEFAULT '',          -- the last whole sync
    lists_at      TEXT NOT NULL DEFAULT '',
    stats         TEXT NOT NULL DEFAULT '{}'         -- what the last sync did, for the page
);

-- Series in the library this person follows. With an account connected, the
-- same follows are on MangaDex.
CREATE TABLE manga_md_follows (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    series_id INTEGER NOT NULL REFERENCES manga_series(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, series_id)
);

-- What both sides agreed on at the last sync, so a change on either side can be
-- told from one that's already there: follows by MangaDex id, read chapters by
-- MangaDex id and normalised chapter number.
CREATE TABLE manga_md_follow_synced (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    dex_id  TEXT NOT NULL,
    PRIMARY KEY (user_id, dex_id)
);
CREATE TABLE manga_md_read_synced (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    dex_id  TEXT NOT NULL,
    number  TEXT NOT NULL,
    PRIMARY KEY (user_id, dex_id, number)
);

-- Followed on MangaDex but not in the library, with why (adult, can't add, ...).
CREATE TABLE manga_md_outside (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    dex_id  TEXT NOT NULL,
    reason  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, dex_id)
);

-- Reading status per person and series: reading, on_hold, plan_to_read, dropped,
-- re_reading, completed, or '' for none. synced is the value both sides last had.
CREATE TABLE manga_md_status (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    series_id INTEGER NOT NULL REFERENCES manga_series(id) ON DELETE CASCADE,
    status    TEXT NOT NULL DEFAULT '',
    synced    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, series_id)
);

-- The person's MDLists, as last read.
CREATE TABLE manga_md_lists (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    list_id    TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    visibility TEXT NOT NULL DEFAULT '',
    manga      TEXT NOT NULL DEFAULT '[]',          -- MangaDex ids, in the list's order
    position   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, list_id)
);

-- New chapters of series the person follows on MangaDex, newest first.
CREATE TABLE manga_md_feed (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chapter_id  TEXT NOT NULL,
    dex_id      TEXT NOT NULL,
    number      TEXT NOT NULL DEFAULT '',
    volume      TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    group_name  TEXT NOT NULL DEFAULT '',
    readable_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, chapter_id)
);

-- What the person read on MangaDex lately, from its reading history.
CREATE TABLE manga_md_history (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chapter_id TEXT NOT NULL,
    read_at    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, chapter_id)
);

-- Shared by everyone, as it's public: which chapter on MangaDex is which, from a
-- series' aggregate or a chapter lookup, and the titles and covers of series seen
-- in follows, lists, the feed or history.
CREATE TABLE manga_md_chapters (
    chapter_id TEXT PRIMARY KEY,
    dex_id     TEXT NOT NULL,
    volume     TEXT NOT NULL DEFAULT '',
    number     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX manga_md_chapters_series ON manga_md_chapters (dex_id);
CREATE TABLE manga_md_aggregated (
    dex_id     TEXT NOT NULL,
    language   TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (dex_id, language)
);
CREATE TABLE manga_md_titles (
    dex_id     TEXT PRIMARY KEY,
    title      TEXT NOT NULL DEFAULT '',
    rating     TEXT NOT NULL DEFAULT '',
    cover      TEXT NOT NULL DEFAULT '',
    year       INTEGER NOT NULL DEFAULT 0,
    anilist_id INTEGER NOT NULL DEFAULT 0,
    fetched_at TEXT NOT NULL DEFAULT ''
);
