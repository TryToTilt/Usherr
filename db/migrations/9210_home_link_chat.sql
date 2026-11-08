-- Usherr: linked screenings, step 4 (home_link_chat.go,
-- internal/link/screening/chat.go): chat and reactions over the link.

-- What was said at a linked screening, on each Usherr at it (hosted here or
-- a guest's), by the screening's id: the last 500 are kept. user_id is the
-- person here who said it (0: someone at another Usherr); house is the
-- Usherr they said it at. pos is the second of the film it was said at
-- (negative in the pre-show).
CREATE TABLE link_chat (
    sid     TEXT    NOT NULL,
    id      TEXT    NOT NULL,
    user_id INTEGER NOT NULL DEFAULT 0,
    name    TEXT    NOT NULL,
    house   TEXT    NOT NULL DEFAULT '',
    text    TEXT    NOT NULL,
    spoiler INTEGER NOT NULL DEFAULT 0,
    pos     REAL    NOT NULL DEFAULT 0,
    at_ms   INTEGER NOT NULL,
    PRIMARY KEY (sid, id)
);
CREATE INDEX link_chat_at ON link_chat (sid, at_ms);

-- Reactions (HA, EEK, WOW, NOPE, CLAP), each pinned to the second of the
-- film it was sent at, by screening and by film (TMDB id), so a film's
-- most-reacted moments can be found later. The last 5,000 per screening
-- are kept.
CREATE TABLE link_reactions (
    sid     TEXT    NOT NULL,
    id      TEXT    NOT NULL,
    tmdb_id INTEGER NOT NULL DEFAULT 0,
    user_id INTEGER NOT NULL DEFAULT 0,
    name    TEXT    NOT NULL,
    house   TEXT    NOT NULL DEFAULT '',
    word    TEXT    NOT NULL,
    pos     REAL    NOT NULL DEFAULT 0,
    at_ms   INTEGER NOT NULL,
    PRIMARY KEY (sid, id)
);
CREATE INDEX link_reactions_at ON link_reactions (sid, at_ms);
CREATE INDEX link_reactions_film ON link_reactions (tmdb_id, pos);
