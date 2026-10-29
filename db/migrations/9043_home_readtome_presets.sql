-- Usherr: Read-to-Me presets. A named way of reading ("Warm narrator",
-- "Thriller"): the engine, the voice (a Kokoro mix too), the speed and how it
-- reads (rtvOptions as JSON, without the chapters). One can be the default.
CREATE TABLE readtome_presets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    engine     TEXT NOT NULL,
    voice      TEXT NOT NULL,
    speed      REAL NOT NULL DEFAULT 1,
    options    TEXT NOT NULL DEFAULT '{}',
    is_default INTEGER NOT NULL DEFAULT 0,
    user_id    INTEGER NOT NULL DEFAULT 0,
    created_ms INTEGER NOT NULL
);
