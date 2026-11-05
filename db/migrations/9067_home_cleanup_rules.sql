-- Usherr: library clean-up rule sets (Settings > Media servers), the job
-- Maintainerr did. Everything here is off until an administrator switches a
-- rule set on.
--
-- A rule set is kept as JSON (name, library or folder, conditions joined by
-- ALL or ANY, what to do, grace days, keep tags); enabled is copied out so the
-- daily job can skip what is off without reading the rest.
CREATE TABLE cleanup_rule_sets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    enabled    INTEGER NOT NULL DEFAULT 0,
    config     TEXT    NOT NULL DEFAULT '{}',
    created_ms INTEGER NOT NULL DEFAULT 0
);

-- The options shared by every rule set (id 1): the most titles one run acts
-- on, whether members may press "Keep it", which media servers get the
-- "Leaving soon" collection, and when the rules last ran. Row 2 is what
-- Usherr put in each server's collection, so it can take it out again.
CREATE TABLE cleanup_options (
    id     INTEGER PRIMARY KEY,
    config TEXT NOT NULL DEFAULT '{}'
);

-- Leaving soon: what a rule set matched, and when it is acted on if it still
-- matches then. unit names what goes: movie:ID, show:SERIES, season:SERIES:N
-- or file:EPISODE (the first episode in an episode file).
CREATE TABLE cleanup_leaving (
    unit        TEXT    PRIMARY KEY,
    rule_set_id INTEGER NOT NULL DEFAULT 0,
    kind        TEXT    NOT NULL,              -- movie, show, season or episode
    title_id    INTEGER NOT NULL DEFAULT 0,    -- movies.id, or series.id
    season      INTEGER NOT NULL DEFAULT 0,
    title       TEXT    NOT NULL DEFAULT '',
    reason      TEXT    NOT NULL DEFAULT '',
    first_ms    INTEGER NOT NULL DEFAULT 0,    -- when it first matched
    act_ms      INTEGER NOT NULL DEFAULT 0     -- acted on after this, if it still matches
);

-- Titles never touched by the rule sets: a movie (movie:ID) or a whole show
-- (show:SERIES), kept by an administrator or by a member's "Keep it".
CREATE TABLE cleanup_exempt (
    unit     TEXT    PRIMARY KEY,
    kind     TEXT    NOT NULL,
    title_id INTEGER NOT NULL DEFAULT 0,
    title    TEXT    NOT NULL DEFAULT '',
    by_user  INTEGER NOT NULL DEFAULT 0,
    by_name  TEXT    NOT NULL DEFAULT '',
    at_ms    INTEGER NOT NULL DEFAULT 0
);
