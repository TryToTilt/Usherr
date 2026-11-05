-- Usherr: the profile editor's Advanced options (internal/quality/extras.go):
-- sizes per quality, release groups, the profile's own language, where it
-- searches, upgrade limits and the Simple mode's size preferences, kept
-- together as one JSON object. '{}' is a profile with none of them set.
ALTER TABLE quality_profiles ADD COLUMN extras TEXT NOT NULL DEFAULT '{}';
