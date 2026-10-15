-- Home fork: each person's Diary: what they watched and when, from Jellyfin
-- (or Emby), logged by hand, or imported from Letterboxd or Trakt. One entry
-- per title per day; a day's episodes of a show are one entry. Stars (1 to
-- 10: half stars out of five) and a note are optional, and count towards
-- the person's taste.
CREATE TABLE home_diary (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,              -- movie or tv
    tmdb_id    INTEGER NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    year       INTEGER NOT NULL DEFAULT 0,
    image      TEXT NOT NULL DEFAULT '',
    watched_on TEXT NOT NULL,              -- YYYY-MM-DD
    dated      INTEGER NOT NULL DEFAULT 1, -- 0: the day it was rated or logged, not watched (left out of Wrapped)
    episodes   INTEGER NOT NULL DEFAULT 0, -- shows: how many that day
    episode    TEXT NOT NULL DEFAULT '',   -- shows: "S2E3" or "S2E3–S2E5"
    runtime    INTEGER NOT NULL DEFAULT 0, -- minutes, all of that day's episodes for a show
    stars      INTEGER NOT NULL DEFAULT 0, -- 0: none
    note       TEXT NOT NULL DEFAULT '',
    rewatch    INTEGER NOT NULL DEFAULT 0,
    source     TEXT NOT NULL DEFAULT 'manual', -- jellyfin, manual, letterboxd, trakt
    hidden     INTEGER NOT NULL DEFAULT 0, -- taken out by the person; kept so the media server doesn't bring it back
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE (user_id, kind, tmdb_id, watched_on)
);
CREATE INDEX home_diary_when ON home_diary (user_id, watched_on);

-- When each person's Diary last read the media server.
CREATE TABLE home_diary_sync (
    user_id   INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    synced_at TEXT NOT NULL
);
