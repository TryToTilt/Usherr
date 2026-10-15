-- Home fork: story arcs (from ComicVine): an arc and its issues in reading
-- order. Issues are tied to the library by their ComicVine ids, so an arc
-- shows what's had, wanted and missing across every series it runs through.
CREATE TABLE comic_arcs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    cv_id       INTEGER NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    publisher   TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    image_url   TEXT NOT NULL DEFAULT '',
    added_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    checked_at  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE comic_arc_issues (
    arc_id      INTEGER NOT NULL REFERENCES comic_arcs(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    issue_cv_id INTEGER NOT NULL,
    volume_cv_id INTEGER NOT NULL,
    series      TEXT NOT NULL DEFAULT '',
    number      TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    cover_date  TEXT NOT NULL DEFAULT '',
    store_date  TEXT NOT NULL DEFAULT '',
    image_url   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (arc_id, position)
);
CREATE INDEX comic_arc_issues_cv ON comic_arc_issues (issue_cv_id);
