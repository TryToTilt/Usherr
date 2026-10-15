-- Home fork: entertainment news for the Dashboard. Stories from the news
-- feeds set in Settings > Extras, each with a short summary the AI wrote
-- and the films, shows, books, comics and music it names.
CREATE TABLE home_news (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    guid         TEXT NOT NULL UNIQUE,
    source       TEXT NOT NULL,
    category     TEXT NOT NULL,
    title        TEXT NOT NULL,
    link         TEXT NOT NULL,
    published_at TEXT NOT NULL,
    teaser       TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    mentions     TEXT NOT NULL DEFAULT '[]',
    state        TEXT NOT NULL DEFAULT 'new', -- new, done (summarised), skipped (over the day's budget)
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX home_news_published ON home_news (published_at);
CREATE INDEX home_news_state ON home_news (state);
