-- Usherr: Sports on Usherr's own engine (no Sportarr): the leagues in the
-- library, their games (from TheSportsDB) and the teams followed.
CREATE TABLE sports_leagues (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    external_id  TEXT NOT NULL UNIQUE,   -- TheSportsDB's idLeague
    name         TEXT NOT NULL,
    alternate    TEXT NOT NULL DEFAULT '', -- other names, comma separated (release titles use them)
    sport        TEXT NOT NULL DEFAULT '',
    country      TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    badge        TEXT NOT NULL DEFAULT '',
    poster       TEXT NOT NULL DEFAULT '',
    banner       TEXT NOT NULL DEFAULT '',
    current      TEXT NOT NULL DEFAULT '', -- the current season
    seasons_json TEXT NOT NULL DEFAULT '[]', -- every season, newest first
    synced_json  TEXT NOT NULL DEFAULT '{}', -- season -> when its games were last fetched (ms)
    monitored    INTEGER NOT NULL DEFAULT 0,
    added_ms     INTEGER NOT NULL
);

CREATE TABLE sports_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    league_id   INTEGER NOT NULL REFERENCES sports_leagues(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL UNIQUE,      -- TheSportsDB's idEvent
    title       TEXT NOT NULL,
    sport       TEXT NOT NULL DEFAULT '',
    home        TEXT NOT NULL DEFAULT '',
    away        TEXT NOT NULL DEFAULT '',
    season      TEXT NOT NULL DEFAULT '',
    round       TEXT NOT NULL DEFAULT '',
    at_ms       INTEGER NOT NULL DEFAULT 0,
    venue       TEXT NOT NULL DEFAULT '',
    image       TEXT NOT NULL DEFAULT '',
    home_score  INTEGER,
    away_score  INTEGER,
    status      TEXT NOT NULL DEFAULT '',
    monitored   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX sports_events_at ON sports_events (at_ms);
CREATE INDEX sports_events_league ON sports_events (league_id, season);

CREATE TABLE sports_teams (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    external_id TEXT NOT NULL UNIQUE,      -- TheSportsDB's idTeam
    name        TEXT NOT NULL,
    sport       TEXT NOT NULL DEFAULT '',
    country     TEXT NOT NULL DEFAULT '',
    badge       TEXT NOT NULL DEFAULT '',
    added_ms    INTEGER NOT NULL
);
