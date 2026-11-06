-- Usherr: Sports downloads on Usherr's own engine (home_sports_get.go,
-- internal/sports). What each league and followed team gets, the games'
-- files in the Sports folder, and the downloads that bring them.

-- A league's monitoring: its quality profile (0 = Sports' default), which
-- events it gets (a comma list of choices: "main,prelims,early" for fight
-- cards, "race,qualifying,sprint,practice" for race weekends; "" = all),
-- and whether only the followed teams' games are wanted.
ALTER TABLE sports_leagues ADD COLUMN profile_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sports_leagues ADD COLUMN parts TEXT NOT NULL DEFAULT '';
ALTER TABLE sports_leagues ADD COLUMN teams_only INTEGER NOT NULL DEFAULT 0;

-- A followed team: whether its games are got, and in which profile.
ALTER TABLE sports_teams ADD COLUMN monitored INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sports_teams ADD COLUMN profile_id INTEGER NOT NULL DEFAULT 0;

-- When the automatic search last looked for a game (ms), to space searches
-- out and spare the indexers' daily limits; and whether a person chose to
-- get it or not (the league's and teams' rules then leave it as it is).
ALTER TABLE sports_events ADD COLUMN searched_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sports_events ADD COLUMN by_hand INTEGER NOT NULL DEFAULT 0;

-- A game's files: one, or one per part of a fight card.
CREATE TABLE sports_files (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id    INTEGER NOT NULL REFERENCES sports_events(id) ON DELETE CASCADE,
    part        TEXT NOT NULL DEFAULT '', -- "Prelims", "Main Card"... or ""
    path        TEXT NOT NULL,
    quality     TEXT NOT NULL DEFAULT '', -- the tier ("WEBDL-1080p")
    size        INTEGER NOT NULL DEFAULT 0,
    release     TEXT NOT NULL DEFAULT '',
    imported_ms INTEGER NOT NULL,
    UNIQUE (event_id, part)
);

-- A download for a game (and a part of it).
CREATE TABLE sports_downloads (
    queue_id INTEGER PRIMARY KEY REFERENCES download_queue(id) ON DELETE CASCADE,
    event_id INTEGER NOT NULL REFERENCES sports_events(id) ON DELETE CASCADE,
    part     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sports_downloads_event ON sports_downloads (event_id);
