-- Usherr: Read-to-Me's copied voices. A short recording of a voice, uploaded
-- in Usherr and kept by it (config/readtome/voices/<name>.wav), which Chatterbox
-- reads like. Usherr gives it to whichever Chatterbox model runs before using
-- it; given lists the models that have this recording. A deleted voice is kept
-- as a row (deleted = 1) so a Chatterbox that still has it doesn't list it.
CREATE TABLE readtome_voices (
    name       TEXT PRIMARY KEY COLLATE NOCASE,
    seconds    REAL NOT NULL DEFAULT 0,
    given      TEXT NOT NULL DEFAULT '',
    deleted    INTEGER NOT NULL DEFAULT 0,
    user_id    INTEGER NOT NULL DEFAULT 0,
    created_ms INTEGER NOT NULL
);
