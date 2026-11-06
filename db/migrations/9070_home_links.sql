-- Usherr: linked screenings, step 1 (home_link.go, internal/link): other
-- Usherrs linked with this one through the Usherr Link Relay.
--
-- A link: the other Usherr's identity (its Ed25519 public key, base64url)
-- and its X25519 key, the name it gives itself and the one given here
-- (nickname, '' for its own), the relay the link uses, linked or unlinked
-- (an unlinked one has its keys forgotten: unlinked_by says who unlinked),
-- and when it was linked and last heard from (unix ms).
CREATE TABLE link_peers (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    their_id    TEXT    NOT NULL DEFAULT '',
    their_x     TEXT    NOT NULL DEFAULT '',
    name        TEXT    NOT NULL DEFAULT '',
    nickname    TEXT    NOT NULL DEFAULT '',
    version     TEXT    NOT NULL DEFAULT '',
    relay       TEXT    NOT NULL DEFAULT '',
    status      TEXT    NOT NULL DEFAULT 'linked', -- linked or unlinked
    unlinked_by TEXT    NOT NULL DEFAULT '',       -- me or them
    created_at  INTEGER NOT NULL DEFAULT 0,
    last_seen   INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX link_peers_their_id ON link_peers (their_id) WHERE their_id <> '';

-- Invite codes made here (role host) and codes typed in here (role join)
-- until their handshake ends. box is the code's hash as the relay knows it
-- (hex SHA-256 of "ulr-box" and the secret); the secret itself is kept
-- encrypted with secret.key, so the code can be shown again and the answer
-- read. state says how it ended: linked, cancelled, used, expired.
CREATE TABLE link_invites (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    role       TEXT    NOT NULL,
    box        TEXT    NOT NULL UNIQUE,
    secret     TEXT    NOT NULL,
    relay      TEXT    NOT NULL,
    created_at INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0,
    used       INTEGER NOT NULL DEFAULT 0,
    state      TEXT    NOT NULL DEFAULT ''
);
