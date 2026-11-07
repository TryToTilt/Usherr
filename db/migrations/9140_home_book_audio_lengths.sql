-- Usherr: the real length of each file of an audiobook, read with ffprobe.
-- rel is the file's path inside the book's folder; size and mtime_ns say which
-- version of the file was read, so a replaced file is read again. seconds is 0
-- when it could not be read yet; after a few tries it is left alone until the
-- file changes.
CREATE TABLE IF NOT EXISTS book_audio_lengths (
    book_id  INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    rel      TEXT    NOT NULL,
    size     INTEGER NOT NULL DEFAULT 0,
    mtime_ns INTEGER NOT NULL DEFAULT 0,
    seconds  REAL    NOT NULL DEFAULT 0,
    tries    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (book_id, rel)
);
