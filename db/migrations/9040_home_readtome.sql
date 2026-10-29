-- Usherr: Read-to-Me. Audiobooks made from ebooks with computer voices. One job
-- is worked on at a time; the pieces already spoken are kept on disk, so a job
-- stopped by a restart carries on where it was.
CREATE TABLE readtome_jobs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id       INTEGER NOT NULL,
    user_id       INTEGER NOT NULL DEFAULT 0,
    engine        TEXT NOT NULL,
    voice         TEXT NOT NULL,
    speed         REAL NOT NULL DEFAULT 1,
    state         TEXT NOT NULL DEFAULT 'queued', -- queued, running, waiting, done, failed, cancelled
    chapter       INTEGER NOT NULL DEFAULT 0,     -- the one being spoken
    chapters      INTEGER NOT NULL DEFAULT 0,
    pieces_done   INTEGER NOT NULL DEFAULT 0,
    pieces_total  INTEGER NOT NULL DEFAULT 0,
    note          TEXT NOT NULL DEFAULT '',       -- what it is doing or waiting for, in words
    error         TEXT NOT NULL DEFAULT '',
    output_path   TEXT NOT NULL DEFAULT '',
    created_ms    INTEGER NOT NULL,
    started_ms    INTEGER NOT NULL DEFAULT 0,
    done_ms       INTEGER NOT NULL DEFAULT 0,
    spoken_ms     INTEGER NOT NULL DEFAULT 0      -- time spent speaking, for the estimate
);
CREATE INDEX readtome_jobs_state ON readtome_jobs(state);
CREATE INDEX readtome_jobs_book ON readtome_jobs(book_id);
