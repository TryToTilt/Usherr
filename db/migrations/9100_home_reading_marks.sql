-- Usherr: bookmarks and highlights, kept per person (internal/readmarks,
-- reading_marks.go). item names what they are in: book:<id> (an ebook, a
-- PDF or a comic in the Books library) or comic:<issue id> (the Comics
-- library). position is where: an EPUB CFI (a highlight's is a range) or
-- "page:<n>" for PDFs and comics.
CREATE TABLE home_reading_marks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item       TEXT NOT NULL,
    kind       TEXT NOT NULL,              -- bookmark or highlight
    position   TEXT NOT NULL,
    label      TEXT NOT NULL DEFAULT '',   -- the chapter or page it is in
    excerpt    TEXT NOT NULL DEFAULT '',   -- a highlight's words
    note       TEXT NOT NULL DEFAULT '',
    color      TEXT NOT NULL DEFAULT '',
    percent    REAL NOT NULL DEFAULT 0,    -- how far into the book, 0 to 100
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX home_reading_marks_item ON home_reading_marks (user_id, item);
