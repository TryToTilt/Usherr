-- A cover chosen by a person for a book. Open Library often has no cover for
-- a book, or only for another edition, so a book can now carry its own:
--   own_cover       0 = none, otherwise the time (in milliseconds) the picture
--                   was saved. It goes into the picture's address, so a browser
--                   that kept the old one fetches the new one.
--   picked_cover_id an Open Library cover id picked from the book's other
--                   editions (0 = none).
-- The automatic cover_id is never changed by either, so a later look-up of the
-- book's details can't undo a choice, and removing the choice brings the
-- automatic cover straight back.
ALTER TABLE books ADD COLUMN own_cover INTEGER NOT NULL DEFAULT 0;
ALTER TABLE books ADD COLUMN picked_cover_id INTEGER NOT NULL DEFAULT 0;

-- The picture itself, kept in the database so a backup carries it. One small
-- JPEG per book, in its own table so listing books never reads it.
CREATE TABLE IF NOT EXISTS book_own_covers (
    book_id INTEGER PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,
    image   BLOB    NOT NULL
);
