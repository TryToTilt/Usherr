-- Usherr: the Manga section. Series, by English title, with details from
-- AniList (or added by hand: anilist_id is empty then). Kept apart from comics.

CREATE TABLE manga_series (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    anilist_id    INTEGER UNIQUE,                    -- empty when added by hand
    title         TEXT NOT NULL,                     -- what the screen shows, English first
    sort_title    TEXT NOT NULL DEFAULT '',
    format        TEXT NOT NULL DEFAULT 'manga',     -- manga, manhwa, manhua, oneshot
    status        TEXT NOT NULL DEFAULT '',          -- ongoing, finished, hiatus, cancelled, upcoming
    start_year    INTEGER NOT NULL DEFAULT 0,
    author        TEXT NOT NULL DEFAULT '',
    publisher_en  TEXT NOT NULL DEFAULT '',          -- an English publisher AniList links to
    buy_url       TEXT NOT NULL DEFAULT '',          -- that publisher's page
    site_url      TEXT NOT NULL DEFAULT '',
    licensed_en   INTEGER NOT NULL DEFAULT -1,       -- 1 known to have an official English edition, -1 unknown
    is_adult      INTEGER NOT NULL DEFAULT 0,
    description   TEXT NOT NULL DEFAULT '',
    cover_url     TEXT NOT NULL DEFAULT '',
    volume_count  INTEGER NOT NULL DEFAULT 0,        -- what AniList says; may be the Japanese count
    monitor_new   INTEGER NOT NULL DEFAULT 1,        -- new volumes are wanted as they turn up
    want_all      INTEGER NOT NULL DEFAULT 0,        -- every volume is wanted, also ones found later
    source_mode   TEXT NOT NULL DEFAULT '',          -- '' my settings, official, fan (also fan scans)
    folder        TEXT NOT NULL DEFAULT '',          -- where its volumes are (empty: not yet decided)
    checked_at    TEXT NOT NULL DEFAULT '',          -- when AniList was last asked
    added_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE manga_aliases (
    series_id  INTEGER NOT NULL REFERENCES manga_series(id) ON DELETE CASCADE,
    alias      TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'synonym',      -- romaji, english, native, synonym
    position   INTEGER NOT NULL DEFAULT 0,           -- the order to try them in
    PRIMARY KEY (series_id, alias)
);
