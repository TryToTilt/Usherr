-- mediarium:rebuild
-- Usherr: Community. A watch party joined by its code, found in the host's
-- Discord server: when the guest's own Usherr bot is in that server too, it
-- finds the party's card and thread by the code. Then the guest sees the
-- party's chat, who hosted and who else is going, and the stamp the host's
-- bot gives at the credits reaches the guest's stub (home_community_guest.go).
ALTER TABLE community_joined ADD COLUMN server     TEXT NOT NULL DEFAULT '';
ALTER TABLE community_joined ADD COLUMN channel    TEXT NOT NULL DEFAULT '';
ALTER TABLE community_joined ADD COLUMN message_id TEXT NOT NULL DEFAULT ''; -- the party's card
ALTER TABLE community_joined ADD COLUMN thread_id  TEXT NOT NULL DEFAULT '';
ALTER TABLE community_joined ADD COLUMN host       TEXT NOT NULL DEFAULT '';
ALTER TABLE community_joined ADD COLUMN going      TEXT NOT NULL DEFAULT ''; -- one per line, as on the card

-- Usherr: a ticket stub its owner removed (You, Ticket stubs). Kept, hidden,
-- so it can be brought back.
ALTER TABLE community_stubs ADD COLUMN hidden_ms INTEGER NOT NULL DEFAULT 0;

-- A joined party's chat is kept like a shared one's, so a chat message
-- belongs to its showtime rather than to a party shared from here.
CREATE TABLE community_messages_new (
    backend     TEXT    NOT NULL DEFAULT 'discord',
    message_id  TEXT    NOT NULL,
    showtime_id INTEGER NOT NULL REFERENCES showtimes(id) ON DELETE CASCADE,
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
INSERT INTO community_messages_new SELECT backend, message_id, showtime_id, author_id, author_name, avatar, text, images, at_ms, edited, from_bot FROM community_messages;
DROP TABLE community_messages;
ALTER TABLE community_messages_new RENAME TO community_messages;
CREATE INDEX community_messages_party ON community_messages (showtime_id, at_ms);
