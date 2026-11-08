-- Usherr: Crews, the crew space between parties (home_crews.go,
-- internal/link/crew). A crew is a named group of linked Usherrs ("houses").
-- The Usherr that made it owns it: it keeps the crew's list, votes and clubs
-- and sends each house the latest. Houses keep the same tables, as the owner
-- last said.
--
-- Every crew hangs off a link (owner_peer), and every house of a crew owned
-- here too (crew_houses.peer_id), so unlinking takes them away.

-- A crew known here. owner_peer is NULL when this Usherr owns it. my_hid is
-- this Usherr's number in it (the owner is 1); state is invited or joined;
-- houses is the houses as the owner last said (JSON, house side). rev goes up
-- with every change of the crew itself (its name, its houses).
CREATE TABLE crews (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    cid        TEXT    NOT NULL UNIQUE,
    name       TEXT    NOT NULL,
    owner_peer INTEGER REFERENCES link_peers(id) ON DELETE CASCADE,
    by_name    TEXT    NOT NULL DEFAULT '',
    by_user    INTEGER NOT NULL DEFAULT 0, -- owner side: who made it here
    rev        INTEGER NOT NULL DEFAULT 1,
    my_hid     INTEGER NOT NULL DEFAULT 1,
    state      TEXT    NOT NULL DEFAULT 'joined',
    houses     TEXT    NOT NULL DEFAULT '[]',
    told_have  TEXT    NOT NULL DEFAULT '', -- house side: the Have it? last sent to the owner
    list_seq   INTEGER NOT NULL DEFAULT 0,  -- house side: the latest list and clubs taken from the owner
    club_seq   INTEGER NOT NULL DEFAULT 0,
    created_ms INTEGER NOT NULL DEFAULT 0,
    updated_ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX crews_owner ON crews (owner_peer);

-- Owner side: each linked Usherr in a crew owned here, its number, whether
-- it joined, who opted in there (JSON names), and when it was last sent
-- everything.
CREATE TABLE crew_houses (
    crew_id INTEGER NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    peer_id INTEGER NOT NULL REFERENCES link_peers(id) ON DELETE CASCADE,
    hid     INTEGER NOT NULL,
    state   TEXT    NOT NULL DEFAULT 'invited',
    people  TEXT    NOT NULL DEFAULT '[]',
    sent_ms INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (crew_id, peer_id),
    UNIQUE (crew_id, hid)
);
CREATE INDEX crew_houses_peer ON crew_houses (peer_id);

-- The people here who opted into a crew. Nobody is in a crew unless they
-- joined it themselves.
CREATE TABLE crew_people (
    crew_id   INTEGER NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_ms INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (crew_id, user_id)
);

-- The crew chat (plain text; the last 500 lines of a crew are kept). card is
-- a film sent with the line (JSON crew.Card), '' for none.
CREATE TABLE crew_chat (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    crew_id INTEGER NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    mid     TEXT    NOT NULL,
    hid     INTEGER NOT NULL,
    who     TEXT    NOT NULL,
    text    TEXT    NOT NULL DEFAULT '',
    card    TEXT    NOT NULL DEFAULT '',
    at_ms   INTEGER NOT NULL,
    UNIQUE (crew_id, mid)
);

-- The shared watchlist. have is each house's Have it? (JSON [{h, has}]):
-- yes, getting or no, never anything else about its library.
CREATE TABLE crew_list (
    crew_id  INTEGER NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    tmdb_id  INTEGER NOT NULL,
    title    TEXT    NOT NULL,
    year     INTEGER NOT NULL DEFAULT 0,
    poster   TEXT    NOT NULL DEFAULT '',
    by_name  TEXT    NOT NULL DEFAULT '',
    by_hid   INTEGER NOT NULL DEFAULT 1,
    added_ms INTEGER NOT NULL DEFAULT 0,
    have     TEXT    NOT NULL DEFAULT '[]',
    PRIMARY KEY (crew_id, tmdb_id)
);

-- Next film votes, open and past (kept for the crew's year in review: who
-- hosted, which club night, what won). choices is the ballot's films (JSON
-- crew.Choice, with who voted for each as last told); host_user, theme and
-- showtime_id are only known in the host's own Usherr.
CREATE TABLE crew_votes (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    crew_id       INTEGER NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    vid           TEXT    NOT NULL UNIQUE,
    host_hid      INTEGER NOT NULL,
    host_name     TEXT    NOT NULL DEFAULT '',
    host_user     INTEGER NOT NULL DEFAULT 0,
    closes_ms     INTEGER NOT NULL,
    at_ms         INTEGER NOT NULL,
    preshow_min   INTEGER NOT NULL DEFAULT 10,
    preshow_theme INTEGER NOT NULL DEFAULT 0,
    club_kid      TEXT    NOT NULL DEFAULT '',
    state         TEXT    NOT NULL DEFAULT 'open', -- open, closed, booked, nobody
    winner_tmdb   INTEGER NOT NULL DEFAULT 0,
    choices       TEXT    NOT NULL DEFAULT '[]',
    showtime_id   INTEGER NOT NULL DEFAULT 0,
    seq           INTEGER NOT NULL DEFAULT 0, -- house side: the latest ballot taken from the owner
    created_ms    INTEGER NOT NULL DEFAULT 0,
    closed_ms     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX crew_votes_crew ON crew_votes (crew_id, state);

-- Owner side: each person's vote (one per person: their house and name).
CREATE TABLE crew_ballots (
    vote_id INTEGER NOT NULL REFERENCES crew_votes(id) ON DELETE CASCADE,
    hid     INTEGER NOT NULL,
    who     TEXT    NOT NULL,
    tmdb_id INTEGER NOT NULL,
    at_ms   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (vote_id, hid, who)
);

-- Film clubs: a recurring night. weekday (0 Sunday) and minute of the day
-- are the owner's local time; every is 1 or 2 weeks; mode is pick (whose
-- turn it is picks) or vote (the crew votes). rotation is who's in, in
-- order (JSON crew.Seat), turn whose pick the next night is; pick their
-- pick (JSON crew.Film, '' for none). streak is nights in a row that
-- happened, nights how many happened. ended_ms: removed (kept for the year
-- in review).
CREATE TABLE crew_clubs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    crew_id     INTEGER NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    kid         TEXT    NOT NULL UNIQUE,
    name        TEXT    NOT NULL,
    weekday     INTEGER NOT NULL,
    minute      INTEGER NOT NULL,
    every       INTEGER NOT NULL DEFAULT 1,
    mode        TEXT    NOT NULL DEFAULT 'vote',
    preshow_min INTEGER NOT NULL DEFAULT 10,
    by_name     TEXT    NOT NULL DEFAULT '',
    by_hid      INTEGER NOT NULL DEFAULT 1,
    next_ms     INTEGER NOT NULL DEFAULT 0,
    rotation    TEXT    NOT NULL DEFAULT '[]',
    turn        INTEGER NOT NULL DEFAULT 0,
    pick        TEXT    NOT NULL DEFAULT '',
    streak      INTEGER NOT NULL DEFAULT 0,
    nights      INTEGER NOT NULL DEFAULT 0,
    ended_ms    INTEGER NOT NULL DEFAULT 0,
    created_ms  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX crew_clubs_crew ON crew_clubs (crew_id);

-- Owner side: each club night as it was arranged (for the year in review):
-- who picked (house and name), how (pick or vote), the film, the vote it
-- became, and whether it happened (held 0: skipped, nobody to pick or no
-- films to vote on).
CREATE TABLE crew_club_nights (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    crew_id    INTEGER NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    kid        TEXT    NOT NULL,
    club_name  TEXT    NOT NULL DEFAULT '',
    night_ms   INTEGER NOT NULL,
    mode       TEXT    NOT NULL,
    picker_hid INTEGER NOT NULL DEFAULT 0,
    picker     TEXT    NOT NULL DEFAULT '',
    tmdb_id    INTEGER NOT NULL DEFAULT 0,
    title      TEXT    NOT NULL DEFAULT '',
    vid        TEXT    NOT NULL DEFAULT '',
    held       INTEGER NOT NULL DEFAULT 1,
    created_ms INTEGER NOT NULL DEFAULT 0,
    UNIQUE (kid, night_ms)
);

-- What the crew is watching: lines from people who chose to share (the last
-- 200 of a crew are kept).
CREATE TABLE crew_activity (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    crew_id INTEGER NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    hid     INTEGER NOT NULL,
    who     TEXT    NOT NULL,
    kind    TEXT    NOT NULL,
    title   TEXT    NOT NULL,
    part    TEXT    NOT NULL DEFAULT '',
    stars   INTEGER NOT NULL DEFAULT 0,
    at_ms   INTEGER NOT NULL
);
CREATE INDEX crew_activity_crew ON crew_activity (crew_id, at_ms);

-- "Share what I watch with my crews": off unless the person turned it on
-- themselves. last_diary is the last Diary entry already shared (set when
-- it's turned on, so nothing from before goes out).
CREATE TABLE crew_sharing (
    user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    share      INTEGER NOT NULL DEFAULT 0,
    set_ms     INTEGER NOT NULL DEFAULT 0,
    last_diary INTEGER NOT NULL DEFAULT 0
);
