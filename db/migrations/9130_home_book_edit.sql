-- Usherr: a person can edit a book's details (internal/books/edit.go). A series
-- set by hand is locked, so the look-ups that fill it in don't change it back.
ALTER TABLE books ADD COLUMN series_locked INTEGER NOT NULL DEFAULT 0;
