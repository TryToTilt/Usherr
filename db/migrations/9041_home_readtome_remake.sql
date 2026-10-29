-- Usherr: Read-to-Me remakes. A job that remakes an audiobook names the file it
-- replaces; the new one takes its place only once it is finished.
ALTER TABLE readtome_jobs ADD COLUMN replaces TEXT NOT NULL DEFAULT '';
