-- Usherr: Read-to-Me's own text for a chapter, put right before it is read
-- (a word the ebook has wrong, a footnote, a line that shouldn't be read).
-- hash is the ebook's chapter text it was made from: when the ebook changes,
-- the edit no longer applies.
CREATE TABLE readtome_edits (
    book_id    INTEGER NOT NULL,
    chapter    INTEGER NOT NULL,
    hash       TEXT NOT NULL,
    text       TEXT NOT NULL,
    user_id    INTEGER NOT NULL DEFAULT 0,
    updated_ms INTEGER NOT NULL,
    PRIMARY KEY (book_id, chapter)
);
