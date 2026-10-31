-- Usherr: which media server a Re-Posterr change is for: '' every one (the
-- same pictures everywhere), 'jellyfin' or 'plex' (different pictures on
-- each, when the household wants that).
ALTER TABLE art_changes ADD COLUMN server TEXT NOT NULL DEFAULT '';
