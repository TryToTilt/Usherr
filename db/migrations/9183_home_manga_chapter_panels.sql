-- Usherr: a chapter's panels, as manga_panels keeps a volume's. manga_chapters comes
-- from a later migration; SQLite takes a reference to a table that isn't there yet.
CREATE TABLE manga_chapter_panels (
    chapter_id INTEGER NOT NULL REFERENCES manga_chapters(id) ON DELETE CASCADE,
    page       INTEGER NOT NULL,
    file_size  INTEGER NOT NULL,
    method     TEXT NOT NULL,
    panels     TEXT NOT NULL,
    rtl        INTEGER NOT NULL,
    PRIMARY KEY (chapter_id, page)
);
