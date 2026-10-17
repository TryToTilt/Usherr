-- Finding the movie or episode that owns a file (changing a folder, locating
-- a file, the delete check, the after-import metadata step) looks it up by
-- path once per file. Without an index each lookup read the whole table.
CREATE INDEX IF NOT EXISTS movies_file_path ON movies (file_path);
CREATE INDEX IF NOT EXISTS episodes_file_path ON episodes (file_path);
