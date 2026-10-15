-- Home fork: covers of books found only on Hardcover (their key is "HC<id>"
-- instead of an Open Library work key, so they have no Open Library cover).

CREATE TABLE home_book_covers (
    book_id INTEGER PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,
    url     TEXT NOT NULL
);
