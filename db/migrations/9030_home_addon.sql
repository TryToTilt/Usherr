-- Usherr: the Stremio and Nuvio add-on. Each person has one private key; it is
-- in the add-on's address (/addon/<key>/manifest.json), so the apps need no
-- sign-in. Making a new key stops the old address working.
CREATE TABLE addon_keys (
    key        TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
