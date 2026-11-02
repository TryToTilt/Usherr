-- Usherr: Community, watch parties. A Showtime shared to the community: the
-- card Usherr posted (its channel and message), the thread under it for the
-- chat, the event on the server's calendar, and which alerts went out.
-- Everyone watches in their own Usherr at the same time.
CREATE TABLE community_parties (
    showtime_id  INTEGER NOT NULL PRIMARY KEY REFERENCES showtimes(id) ON DELETE CASCADE,
    backend      TEXT    NOT NULL DEFAULT 'discord',
    server       TEXT    NOT NULL DEFAULT '',
    channel      TEXT    NOT NULL,
    message_id   TEXT    NOT NULL,
    thread_id    TEXT    NOT NULL DEFAULT '',
    event_id     TEXT    NOT NULL DEFAULT '',
    host         TEXT    NOT NULL DEFAULT '', -- who booked it, as people see them there
    told         TEXT    NOT NULL DEFAULT '', -- the last alert posted: preshow, playing or done
    created_ms   INTEGER NOT NULL
);

-- Who answered a party's card: I'm in (in) or Can't make it (out).
CREATE TABLE community_party_people (
    showtime_id INTEGER NOT NULL REFERENCES community_parties(showtime_id) ON DELETE CASCADE,
    external_id TEXT    NOT NULL,
    name        TEXT    NOT NULL DEFAULT '',
    answer      TEXT    NOT NULL,
    at_ms       INTEGER NOT NULL,
    PRIMARY KEY (showtime_id, external_id)
);
