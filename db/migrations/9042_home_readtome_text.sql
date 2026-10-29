-- Usherr: Read-to-Me, how a book is read. A job's choices (the chapters, the
-- spoken title and chapter titles) as JSON, and the household's pronunciation
-- list: a word and how to say it, for one book (book_id) or every book (0).
ALTER TABLE readtome_jobs ADD COLUMN options TEXT NOT NULL DEFAULT '{}';
CREATE TABLE readtome_say (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id INTEGER NOT NULL DEFAULT 0,
    word    TEXT NOT NULL,
    say     TEXT NOT NULL,
    UNIQUE (book_id, word)
);
