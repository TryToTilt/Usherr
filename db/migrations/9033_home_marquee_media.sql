-- Usherr: your own Marquee slides and pre-show pieces. A picture or a video
-- (the file is kept in the config folder, marquee-media), a web page, or a
-- slide of your own words. "slide" ones take turns in the Slides scene,
-- "preshow" ones play in the pre-show between the built-in pieces.
CREATE TABLE marquee_media (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    place      TEXT NOT NULL DEFAULT 'slide', -- 'slide' or 'preshow'
    kind       TEXT NOT NULL,                 -- 'image', 'video', 'web' or 'text'
    title      TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',      -- a text slide's words
    url        TEXT NOT NULL DEFAULT '',      -- a web page
    file       TEXT NOT NULL DEFAULT '',      -- the stored file's name
    mime       TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    seconds    INTEGER NOT NULL DEFAULT 12,   -- a video plays to its end
    enabled    INTEGER NOT NULL DEFAULT 1,
    sort       INTEGER NOT NULL DEFAULT 0,
    created_ms INTEGER NOT NULL DEFAULT 0
);
