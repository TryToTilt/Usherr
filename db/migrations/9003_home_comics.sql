-- Home fork: the Comics module. Series ("volumes" in ComicVine's words) and
-- their issues, with details from ComicVine. Named 9003_home_* so it never
-- collides with an upstream migration number.

CREATE TABLE comic_series (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    cv_id        INTEGER NOT NULL UNIQUE,          -- ComicVine volume id
    name         TEXT NOT NULL,
    start_year   INTEGER NOT NULL DEFAULT 0,
    publisher    TEXT NOT NULL DEFAULT '',
    issue_count  INTEGER NOT NULL DEFAULT 0,
    description  TEXT NOT NULL DEFAULT '',
    image_url    TEXT NOT NULL DEFAULT '',
    folder       TEXT NOT NULL DEFAULT '',          -- where its issues go (empty: the default layout)
    monitor_new  INTEGER NOT NULL DEFAULT 1,        -- new issues are wanted as they come out
    checked_at   TEXT NOT NULL DEFAULT '',          -- when the issue list was last fetched
    added_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE comic_issues (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id   INTEGER NOT NULL REFERENCES comic_series(id) ON DELETE CASCADE,
    cv_id       INTEGER NOT NULL UNIQUE,            -- ComicVine issue id
    number      TEXT NOT NULL DEFAULT '',           -- as printed: "1", "12.1", "½", "Annual 1"
    sort_key    REAL NOT NULL DEFAULT 0,
    title       TEXT NOT NULL DEFAULT '',
    cover_date  TEXT NOT NULL DEFAULT '',           -- YYYY-MM-DD
    store_date  TEXT NOT NULL DEFAULT '',
    image_url   TEXT NOT NULL DEFAULT '',
    wanted      INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'missing',    -- missing, downloading, downloaded
    file_path   TEXT NOT NULL DEFAULT '',
    file_size   INTEGER NOT NULL DEFAULT 0,
    added_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX comic_issues_series ON comic_issues(series_id, sort_key);
CREATE INDEX comic_issues_wanted ON comic_issues(wanted, status);

-- Which download is for which issue (kept apart from download_queue).
CREATE TABLE comic_downloads (
    queue_id INTEGER PRIMARY KEY REFERENCES download_queue(id) ON DELETE CASCADE,
    issue_id INTEGER NOT NULL REFERENCES comic_issues(id) ON DELETE CASCADE
);
CREATE INDEX comic_downloads_issue ON comic_downloads(issue_id);
