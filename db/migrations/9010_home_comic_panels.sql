-- Home fork: the panels on each page of a comic, for the reader's guided
-- view, worked out once (by the AI panel detector, or the gutter method when
-- the model can't be had) and kept. file_size ties them to the issue's file:
-- a replaced file is worked out again.
CREATE TABLE comic_panels (
    issue_id  INTEGER NOT NULL REFERENCES comic_issues(id) ON DELETE CASCADE,
    page      INTEGER NOT NULL,
    file_size INTEGER NOT NULL,
    method    TEXT NOT NULL,
    panels    TEXT NOT NULL,
    PRIMARY KEY (issue_id, page)
);
