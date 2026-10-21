-- Usherr: the screens showing the Marquee (a tablet, a portrait monitor...).
-- Each browser that opens /marquee remembers an id and shows up here, so the
-- phone remote can name it, give it its own settings and steer it.
CREATE TABLE marquee_screens (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    settings   TEXT NOT NULL DEFAULT '', -- JSON marqueeSettings; '' uses the shared settings
    created_ms INTEGER NOT NULL DEFAULT 0,
    seen_ms    INTEGER NOT NULL DEFAULT 0,
    width      INTEGER NOT NULL DEFAULT 0,
    height     INTEGER NOT NULL DEFAULT 0
);
