-- Home fork: Discover recipes (saved filters, each its own Discover row) and
-- the streaming services each person pays for.
CREATE TABLE home_recipes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,            -- movie, tv, anime
    name       TEXT NOT NULL,
    filters    TEXT NOT NULL DEFAULT '{}',
    shared     INTEGER NOT NULL DEFAULT 0, -- the whole household sees it
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX home_recipes_user ON home_recipes (user_id);

CREATE TABLE home_user_services (
    user_id   INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    providers TEXT NOT NULL DEFAULT '[]' -- TMDB provider ids
);
