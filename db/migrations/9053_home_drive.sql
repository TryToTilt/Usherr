-- Usherr: the virtual drive (for Plex). A streamed file is on the drive once
-- its release is kept (nzb, size, inner_file). These say when the drive's own
-- search last tried a file that has none, so one with nothing on the
-- indexers isn't searched again every few minutes.
ALTER TABLE streamed_files ADD COLUMN drive_tried_at TEXT NOT NULL DEFAULT '';
ALTER TABLE streamed_files ADD COLUMN drive_tries INTEGER NOT NULL DEFAULT 0;
