-- Home fork: genres for books (the AI tags each book once, for the Library's
-- "Rows by: Genre"), and the story arcs each comic issue belongs to (read
-- once from ComicVine, for the "Story arcs for you" row).
CREATE TABLE home_book_genres (
    book_id    INTEGER PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,
    genres     TEXT NOT NULL DEFAULT '', -- "|" separated, from the fixed list
    tagged_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE home_comic_issue_arcs (
    issue_cv_id INTEGER PRIMARY KEY,
    arcs        TEXT NOT NULL DEFAULT '[]', -- [{"cvId":1,"name":"..."}]
    checked_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
