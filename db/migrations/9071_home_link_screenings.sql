-- Usherr: linked screenings, step 2 (home_link_screenings.go,
-- internal/link/screening): showtimes shared with linked Usherrs.
--
-- Every row hangs off a link (link_peers.id), so unlinking takes a peer's
-- screenings away on both sides (ForgetPeer deletes them; removing the link
-- row cascades).

-- Host side: a showtime booked here and shared over the link. sid is the
-- screening's id as linked Usherrs know it; skey is the screening key
-- (encrypted with secret.key), made new when a guest leaves; rev goes up with
-- every change the guests are told about; copy_min is this Usherr's own
-- copy's runtime (0 when unknown).
CREATE TABLE link_screenings_out (
    showtime_id INTEGER PRIMARY KEY REFERENCES showtimes(id) ON DELETE CASCADE,
    sid         TEXT    NOT NULL UNIQUE,
    skey        TEXT    NOT NULL,
    rev         INTEGER NOT NULL DEFAULT 1,
    copy_min    INTEGER NOT NULL DEFAULT 0,
    created_ms  INTEGER NOT NULL DEFAULT 0
);

-- Each linked Usherr invited to one, and what it answered: answer (going,
-- not, maybe; '' not yet) for how many of its household, has the film (yes,
-- getting, no; '' not told yet) and its copy's runtime. ready_rev is the
-- last revision it said it saw; sent_ms when the screening was last sent to
-- it (sent again until it says it saw the latest).
CREATE TABLE link_screening_guests (
    showtime_id INTEGER NOT NULL REFERENCES link_screenings_out(showtime_id) ON DELETE CASCADE,
    peer_id     INTEGER NOT NULL REFERENCES link_peers(id) ON DELETE CASCADE,
    answer      TEXT    NOT NULL DEFAULT '',
    count       INTEGER NOT NULL DEFAULT 0,
    has         TEXT    NOT NULL DEFAULT '',
    runtime     INTEGER NOT NULL DEFAULT 0,
    ready_rev   INTEGER NOT NULL DEFAULT 0,
    sent_ms     INTEGER NOT NULL DEFAULT 0,
    answered_ms INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (showtime_id, peer_id)
);
CREATE INDEX link_screening_guests_peer ON link_screening_guests (peer_id);

-- Guest side: a screening a linked Usherr invited this one to, as its host
-- last said (film, start, pre-show, who hosts), with its key (encrypted; kid
-- is the key's id, hex, to find it when a message sealed with it comes) and
-- the latest roster (JSON). has / my_runtime / my_file are this Usherr's own
-- copy as last checked, and told what was last sent to the host.
CREATE TABLE link_screenings_in (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    peer_id     INTEGER NOT NULL REFERENCES link_peers(id) ON DELETE CASCADE,
    sid         TEXT    NOT NULL UNIQUE,
    kid         TEXT    NOT NULL,
    skey        TEXT    NOT NULL,
    rev         INTEGER NOT NULL DEFAULT 1,
    tmdb_id     INTEGER NOT NULL,
    title       TEXT    NOT NULL,
    year        INTEGER NOT NULL DEFAULT 0,
    poster_url  TEXT    NOT NULL DEFAULT '',
    runtime     INTEGER NOT NULL DEFAULT 0,
    copy_min    INTEGER NOT NULL DEFAULT 0,
    edition     TEXT    NOT NULL DEFAULT '',
    at_ms       INTEGER NOT NULL,
    preshow_min INTEGER NOT NULL DEFAULT 0,
    host_name   TEXT    NOT NULL DEFAULT '',
    roster      TEXT    NOT NULL DEFAULT '[]',
    has         TEXT    NOT NULL DEFAULT '',
    my_runtime  INTEGER NOT NULL DEFAULT 0,
    my_file     TEXT    NOT NULL DEFAULT '',
    told        TEXT    NOT NULL DEFAULT '',
    created_ms  INTEGER NOT NULL DEFAULT 0,
    updated_ms  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX link_screenings_in_kid ON link_screenings_in (kid);
CREATE INDEX link_screenings_in_peer ON link_screenings_in (peer_id);

-- Guest side: each person's own answer here (the household's answer to the
-- host is made from these).
CREATE TABLE link_screening_people (
    screening_id INTEGER NOT NULL REFERENCES link_screenings_in(id) ON DELETE CASCADE,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    answer       TEXT    NOT NULL,
    at_ms        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (screening_id, user_id)
);
