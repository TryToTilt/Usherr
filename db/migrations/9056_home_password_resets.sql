-- Usherr: links for setting a new password by email (Forgot password? on the
-- sign-in page). Only a SHA-256 hash of each link's secret is kept; a link
-- works once (used_at), for 30 minutes (expires_at), and a newer link or a
-- password set another way ends the account's older ones.
CREATE TABLE password_resets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT    NOT NULL UNIQUE,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL,
    used_at    TEXT
);
CREATE INDEX password_resets_user ON password_resets(user_id);
