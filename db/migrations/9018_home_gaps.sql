-- Home fork: "Complete your library" (after CinePlete). Who made and leads
-- each film the household has, and what TMDB recommends after it (read once
-- per film); the latest scan's findings; and what each person ignores.
CREATE TABLE home_film_people (
    tmdb_id    INTEGER PRIMARY KEY,
    directors  TEXT NOT NULL DEFAULT '[]', -- [{"id":1,"name":"..."}]
    leads      TEXT NOT NULL DEFAULT '[]', -- the first five billed
    recs       TEXT NOT NULL DEFAULT '[]', -- recommended TMDB ids
    fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE home_gaps (
    id       INTEGER PRIMARY KEY CHECK (id = 1),
    data     TEXT NOT NULL,
    built_at TEXT NOT NULL
);

CREATE TABLE home_gap_ignores (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind    TEXT NOT NULL, -- franchise, director, actor
    ref_id  INTEGER NOT NULL,
    name    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, kind, ref_id)
);
