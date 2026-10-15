-- Home fork: the week's new comics from ComicVine, kept per week so the page
-- is quick and ComicVine's hourly allowance is spared.
CREATE TABLE comic_week_cache (
    week_start TEXT PRIMARY KEY, -- the Monday, YYYY-MM-DD
    fetched_at TEXT NOT NULL,
    data       TEXT NOT NULL DEFAULT '[]'
);
