-- Home fork: comic search results that may be the issue but aren't sure
-- enough to grab by themselves wait here for a person (the review list),
-- and the last search of each issue is kept with why each release was or
-- wasn't taken.

CREATE TABLE comic_reviews (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id     INTEGER NOT NULL REFERENCES comic_issues(id) ON DELETE CASCADE,
    title        TEXT NOT NULL,
    download_url TEXT NOT NULL,
    size_bytes   INTEGER NOT NULL DEFAULT 0,
    protocol     TEXT NOT NULL DEFAULT 'usenet',
    indexer      TEXT NOT NULL DEFAULT '',
    score        INTEGER NOT NULL DEFAULT 0,
    notes        TEXT NOT NULL DEFAULT '[]',
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (issue_id, download_url)
);
CREATE INDEX comic_reviews_issue ON comic_reviews (issue_id);

CREATE TABLE comic_search_log (
    issue_id    INTEGER PRIMARY KEY REFERENCES comic_issues(id) ON DELETE CASCADE,
    searched_at TEXT NOT NULL,
    outcome     TEXT NOT NULL DEFAULT '', -- grabbed | review | nothing
    details     TEXT NOT NULL DEFAULT '{}'
);
