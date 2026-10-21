-- When a browser session was last used, for the optional "sign out after being
-- idle" setting. NULL (every session from before this) counts as the time it was
-- created. It is written at most once a minute per session.
ALTER TABLE sessions ADD COLUMN last_used_at TEXT;

-- Set when a session was found idle for too long. The row stays until it would
-- have expired anyway, so the person who comes back is told why they were signed
-- out, and a later change of the setting does not bring the session back.
ALTER TABLE sessions ADD COLUMN idle_ended_at TEXT;
