-- Usherr: Re-Posterr's own posters (home_artstyle.go, M5). A change can be a
-- picture made in the household's style (style: the style's fingerprint, ''
-- for a picture put on as it is), and a show's season posters and episode
-- title cards are changes of their own (type seasons or cards).
ALTER TABLE art_changes ADD COLUMN style TEXT NOT NULL DEFAULT '';
