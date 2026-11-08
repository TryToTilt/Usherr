-- Usherr: the party at a showtime (home_link_party.go,
-- internal/link/screening/party.go): the lobby, the intermission and the
-- verdict at the credits. A party is keyed by pkey: a linked screening's id
-- (its sid), or "st-<showtime id>" for a showtime of this Usherr's own.

-- People here who took their seat in a party's lobby.
CREATE TABLE party_seated (
    pkey      TEXT    NOT NULL,
    user_id   INTEGER NOT NULL,
    seated_ms INTEGER NOT NULL,
    PRIMARY KEY (pkey, user_id)
);

-- The host of a linked screening: each guest Usherr's people at the lobby,
-- as its lobby.seat said (a JSON list of {id, name, seated}, in the order
-- they said I'm in).
CREATE TABLE party_guest_people (
    sid     TEXT    NOT NULL,
    peer_id INTEGER NOT NULL,
    people  TEXT    NOT NULL DEFAULT '[]',
    at_ms   INTEGER NOT NULL,
    PRIMARY KEY (sid, peer_id)
);

-- One row per party. A guest keeps the host's seat map (roster), its
-- intermission and the crew's verdict (tally) as they last came; the host
-- keeps its own intermission. inter_until is this Usherr's clock. credits_ms:
-- the host rolled the credits (a showtime of this Usherr's own).
CREATE TABLE party_state (
    pkey        TEXT    PRIMARY KEY,
    roster      TEXT    NOT NULL DEFAULT '',
    roster_seq  INTEGER NOT NULL DEFAULT 0,
    inter_seq   INTEGER NOT NULL DEFAULT 0,
    inter_on    INTEGER NOT NULL DEFAULT 0,
    inter_until INTEGER NOT NULL DEFAULT 0,
    statuses    TEXT    NOT NULL DEFAULT '[]',
    tally       TEXT    NOT NULL DEFAULT '',
    tally_seq   INTEGER NOT NULL DEFAULT 0,
    credits_ms  INTEGER NOT NULL DEFAULT 0
);

-- The host: everyone's status at the intermission ("getting snacks"). who
-- is "u<user id>" for someone here, "p<peer row>:<token>" for someone at a
-- guest Usherr.
CREATE TABLE party_status (
    pkey   TEXT    NOT NULL,
    who    TEXT    NOT NULL,
    name   TEXT    NOT NULL,
    house  TEXT    NOT NULL DEFAULT '',
    status TEXT    NOT NULL,
    at_ms  INTEGER NOT NULL,
    PRIMARY KEY (pkey, who)
);

-- Verdicts: stars (1 to 10, half stars out of five) for the party's film.
-- Every Usherr keeps its own people's (user_id > 0, with their line, which
-- never leaves this Usherr); the host also keeps each guest's people's
-- (user_id 0, voter "p<peer row>:<token>", a name only when they share).
-- The stars also go to the person's Diary (home_diary.stars). sent_ms: a
-- guest's vote got to the host.
CREATE TABLE party_verdicts (
    pkey    TEXT    NOT NULL,
    voter   TEXT    NOT NULL,
    user_id INTEGER NOT NULL DEFAULT 0,
    tmdb_id INTEGER NOT NULL DEFAULT 0,
    name    TEXT    NOT NULL DEFAULT '',
    house   TEXT    NOT NULL DEFAULT '',
    stars   INTEGER NOT NULL,
    line    TEXT    NOT NULL DEFAULT '',
    share   INTEGER NOT NULL DEFAULT 0,
    at_ms   INTEGER NOT NULL,
    sent_ms INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (pkey, voter)
);
CREATE INDEX party_verdicts_user ON party_verdicts (user_id, at_ms);
