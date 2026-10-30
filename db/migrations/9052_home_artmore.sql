-- Usherr: Re-Posterr's own pictures with the household's own words on them
-- (home_artmore.go): the words, '' for the title as Jellyfin has it.
ALTER TABLE art_changes ADD COLUMN words TEXT NOT NULL DEFAULT '';
