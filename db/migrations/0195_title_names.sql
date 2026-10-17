-- The other names of a movie or a show, for searching indexers and matching
-- releases. A release of a film made in Germany is often named by its German
-- title ("Der Herr der Ringe 2001 German ..."), so a search for the English
-- title alone never finds it. TMDB lists those titles; the few that matter for
-- the audio languages the person wants are kept here, at most four per title.
--   kind        'movie' or 'tv'
--   names       a JSON list of titles, empty for a title with nothing extra
--   languages   the audio language list the titles were picked for; when the
--               list in Settings changes, the titles are picked again
--   checked_at  when TMDB was last asked (RFC 3339), so a title with no extra
--               names is not asked about again every few minutes
CREATE TABLE IF NOT EXISTS title_names (
    kind       TEXT    NOT NULL,
    tmdb_id    INTEGER NOT NULL,
    names      TEXT    NOT NULL DEFAULT '',
    languages  TEXT    NOT NULL DEFAULT '',
    checked_at TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (kind, tmdb_id)
);
