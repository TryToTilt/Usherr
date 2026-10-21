-- Indexer priority is now a number from 1 to 50 (1 = tried first, 25 = the
-- default, like Prowlarr). The old three-level column stays untouched so an
-- older build still reads its own values after a downgrade.
ALTER TABLE indexers ADD COLUMN priority_rank INTEGER NOT NULL DEFAULT 25;
UPDATE indexers SET priority_rank = CASE priority WHEN 1 THEN 10 WHEN 3 THEN 40 ELSE 25 END;
