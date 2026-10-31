-- Usherr: who the Diary last read for an account (its Jellyfin user and
-- Plex accounts). When that changes, say a Plex account is linked, the
-- whole history is read again, so what was watched before isn't missed.
ALTER TABLE home_diary_sync ADD COLUMN person TEXT NOT NULL DEFAULT '';
