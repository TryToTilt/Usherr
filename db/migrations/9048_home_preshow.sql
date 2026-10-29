-- Usherr: the pre-show (home_preshow.go). Clips are the video files in the
-- pre-show folder, one folder per category (a holiday's folder plays in its
-- season); themes are the pre-show as a list of blocks (JSON, see
-- preshowBlock); schedules say when a theme is the one that plays.
CREATE TABLE preshow_clips (
    id        INTEGER PRIMARY KEY,
    path      TEXT NOT NULL UNIQUE, -- in the pre-show folder, with "/"
    category  TEXT NOT NULL DEFAULT '',
    title     TEXT NOT NULL,
    seconds   REAL NOT NULL DEFAULT 0,
    width     INTEGER NOT NULL DEFAULT 0,
    height    INTEGER NOT NULL DEFAULT 0,
    size      INTEGER NOT NULL DEFAULT 0,
    enabled   INTEGER NOT NULL DEFAULT 1,
    added_ms  INTEGER NOT NULL
);

CREATE TABLE preshow_themes (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    blocks     TEXT NOT NULL,
    updated_ms INTEGER NOT NULL
);

CREATE TABLE preshow_schedules (
    id       INTEGER PRIMARY KEY,
    name     TEXT NOT NULL,
    theme_id INTEGER NOT NULL,
    rule     TEXT NOT NULL, -- JSON, see preshowRule
    priority INTEGER NOT NULL DEFAULT 0,
    enabled  INTEGER NOT NULL DEFAULT 1
);

-- When a clip or a trailer last played, so the same ones don't come round
-- every night: item is clip:<id> or trailer:<tmdb>.
CREATE TABLE preshow_played (
    item      TEXT PRIMARY KEY,
    played_ms INTEGER NOT NULL
);

-- A booking's own theme (0: the one the schedule picks).
ALTER TABLE showtimes ADD COLUMN preshow_theme INTEGER NOT NULL DEFAULT 0;

-- Ready to use: a full evening and a short one.
INSERT INTO preshow_themes (name, blocks, updated_ms) VALUES
 ('Movie night', '[{"type":"card","card":"welcome"},{"type":"seasonal","count":1},{"type":"clips","category":"Bumpers","count":1},{"type":"card","card":"silence"},{"type":"card","card":"coming"},{"type":"trailers","count":3},{"type":"clips","category":"Sound","count":1,"when":{"sound":["atmos","dtsx"]}},{"type":"card","card":"rating"},{"type":"card","card":"countdown"},{"type":"card","card":"feature"}]', 0),
 ('Quick start', '[{"type":"card","card":"silence"},{"type":"trailers","count":1},{"type":"card","card":"feature"}]', 0);
