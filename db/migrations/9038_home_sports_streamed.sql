-- Usherr: the games streamed (Sports), one row a game: the Dashboard's
-- Sports card counts them next to the games downloaded.
CREATE TABLE sports_streamed (
    backend  TEXT NOT NULL,
    event_id TEXT NOT NULL,
    title    TEXT NOT NULL,
    league   TEXT NOT NULL DEFAULT '',
    user_id  INTEGER NOT NULL DEFAULT 0,
    at_ms    INTEGER NOT NULL,
    PRIMARY KEY (backend, event_id)
);
