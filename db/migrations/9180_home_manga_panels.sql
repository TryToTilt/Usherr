-- Usherr: a manga page's panels, worked out once for the reader's panel-by-panel
-- view, as comic_panels does for Comics. Another copy of the file is another
-- file_size, so it is worked out again.
CREATE TABLE manga_panels (
    volume_id INTEGER NOT NULL REFERENCES manga_volumes(id) ON DELETE CASCADE,
    page      INTEGER NOT NULL,
    file_size INTEGER NOT NULL,
    method    TEXT NOT NULL,
    panels    TEXT NOT NULL,
    rtl       INTEGER NOT NULL,
    PRIMARY KEY (volume_id, page)
);
