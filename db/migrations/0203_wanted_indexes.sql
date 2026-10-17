-- The Dashboard and Wanted count the episodes still wanted: monitored, marked
-- missing and aired. On a big TV library that is hundreds of thousands of
-- rows, and reading each one (overview and all) took seconds. This index
-- holds only those episodes, with the columns the count needs, so the count
-- never touches the table.
CREATE INDEX IF NOT EXISTS episodes_wanted ON episodes (series_id, air_date, file_path) WHERE status = 'missing' AND monitored = 1;

-- The TV list adds up the size of every show's files. This index holds only
-- the episodes that name a file, so finding them is no longer a walk through
-- the whole table.
CREATE INDEX IF NOT EXISTS episodes_with_file ON episodes (series_id, file_path) WHERE file_path IS NOT NULL AND file_path != '';
