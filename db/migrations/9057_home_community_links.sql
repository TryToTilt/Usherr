-- Usherr: Community. Which Discord account belongs to which Usherr account,
-- linked by the person themselves (/link in Discord gives a code they type
-- into Usherr). One Discord account per Usherr account and the other way
-- round. backend says where the account is ("discord"), so another backend
-- can be added later without moving anything.
CREATE TABLE community_links (
    user_id     INTEGER NOT NULL PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    backend     TEXT    NOT NULL DEFAULT 'discord',
    external_id TEXT    NOT NULL,
    name        TEXT    NOT NULL DEFAULT '',
    avatar      TEXT    NOT NULL DEFAULT '',
    linked_at   TEXT    NOT NULL,
    UNIQUE (backend, external_id)
);
