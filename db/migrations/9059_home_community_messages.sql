-- Usherr: Community, the chat. Usherr's own copy of what's said in each watch
-- party's thread, so the Showtime page and the phone page can show it (and
-- still can after the party, or after a move to another server).
CREATE TABLE community_messages (
    backend     TEXT    NOT NULL DEFAULT 'discord',
    message_id  TEXT    NOT NULL,
    showtime_id INTEGER NOT NULL REFERENCES community_parties(showtime_id) ON DELETE CASCADE,
    author_id   TEXT    NOT NULL DEFAULT '',
    author_name TEXT    NOT NULL DEFAULT '',
    avatar      TEXT    NOT NULL DEFAULT '',
    text        TEXT    NOT NULL DEFAULT '',
    images      TEXT    NOT NULL DEFAULT '', -- picture addresses, one per line
    at_ms       INTEGER NOT NULL,
    edited      INTEGER NOT NULL DEFAULT 0,
    from_bot    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (backend, message_id)
);
CREATE INDEX community_messages_party ON community_messages (showtime_id, at_ms);
