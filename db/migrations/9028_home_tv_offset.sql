-- Usherr: a program can be part of a file (a show split by commercial
-- breaks): offset_ms is where in the file it starts.
ALTER TABLE tv_programs ADD COLUMN offset_ms INTEGER NOT NULL DEFAULT 0;
