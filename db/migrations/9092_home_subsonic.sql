-- Usherr: the Subsonic / OpenSubsonic API for phone apps (subsonic*.go).
-- Modelled on DroppedNeedle's compat_play_queues (AGPL-3.0).

-- Whether an account may sign in from Subsonic apps. Off until the person
-- switches it on.
CREATE TABLE IF NOT EXISTS subsonic_users (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL DEFAULT 0
);

-- Music apps sign in with the account's one app password, the same one
-- e-reader apps use (ereader_access, internal/ereader), never the account's
-- own password. Subsonic's token sign-in sends md5(password + salt), so the
-- server needs the password back: it is now also kept encrypted with the
-- server key. A password made before this has only its hash; it still works
-- with p= and apiKey, and a new one is needed for token sign-in.
ALTER TABLE ereader_access ADD COLUMN opds_secret_enc TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS ereader_opds_hash ON ereader_access (opds_hash) WHERE opds_hash <> '';

-- The play queue a phone app saves, to carry on from another device.
CREATE TABLE IF NOT EXISTS subsonic_play_queues (
    user_id     INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    current     INTEGER NOT NULL DEFAULT 0, -- track id
    position_ms INTEGER NOT NULL DEFAULT 0,
    track_ids   TEXT    NOT NULL DEFAULT '[]', -- JSON array, in order
    changed_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    changed_by  TEXT    NOT NULL DEFAULT ''
);
