-- A change marker for the book list. The e-reader catalogue (OPDS) and the Kobo
-- sync keep what they built in memory and throw it away when this number moves.
-- It is bumped by triggers, so every way of changing a book (the repo, the
-- scanner, an import, a restore) moves it without each writer having to know.
-- Reading progress is in its own table and does not touch it.
CREATE TABLE books_change (
    id      INTEGER PRIMARY KEY CHECK (id = 1),
    version INTEGER NOT NULL
);
INSERT INTO books_change (id, version) VALUES (1, 1);

CREATE TRIGGER books_change_insert AFTER INSERT ON books
BEGIN
    UPDATE books_change SET version = version + 1 WHERE id = 1;
END;

CREATE TRIGGER books_change_delete AFTER DELETE ON books
BEGIN
    UPDATE books_change SET version = version + 1 WHERE id = 1;
END;

-- Only the columns the e-reader feeds read (the audiobook columns and the
-- wanted flags are left out, so a download in progress does not clear the cache).
CREATE TRIGGER books_change_update AFTER UPDATE OF
    ol_key, title, author, year, cover_id, description, added_at,
    ebook_status, ebook_format, ebook_path, series_name, series_position,
    own_cover, picked_cover_id
ON books
BEGIN
    UPDATE books_change SET version = version + 1 WHERE id = 1;
END;
