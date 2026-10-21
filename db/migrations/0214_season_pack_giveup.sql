-- A season that keeps finding no season pack is given up on after a few
-- searches (until an episode is added to it, a pack setting changes, or two
-- months have passed). tries counts the searches that found nothing, episodes
-- is how many episodes the season had at the last search.
ALTER TABLE season_pack_searches ADD COLUMN tries    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE season_pack_searches ADD COLUMN episodes INTEGER NOT NULL DEFAULT 0;
