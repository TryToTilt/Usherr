-- Usherr: linked screenings, step 3 (home_link_room.go,
-- internal/link/screening/playback.go): synced playback.

-- Guest side: the booking in this Usherr's own Showtime, made when its
-- people say they're in (NULL until then, and again when nobody is), so the
-- Marquee counts down to it like a local one. recipe is the host's pre-show
-- (JSON items), built here from this Usherr's own trailers and cards.
ALTER TABLE link_screenings_in ADD COLUMN showtime_id INTEGER;
ALTER TABLE link_screenings_in ADD COLUMN recipe TEXT NOT NULL DEFAULT '';
CREATE INDEX link_screenings_in_showtime ON link_screenings_in (showtime_id);

-- Host side: the pre-show as it will play here and everywhere (a plan,
-- JSON; '' until it's made, shortly before), and whether guests may ask to
-- pause.
ALTER TABLE link_screenings_out ADD COLUMN recipe TEXT NOT NULL DEFAULT '';
ALTER TABLE link_screenings_out ADD COLUMN anyone_pause INTEGER NOT NULL DEFAULT 0;

-- Both sides: where the film is, as the host last said. play_seq 0: nothing
-- said yet (the schedule holds); play_at is this Usherr's own clock (unix
-- ms), play_pos seconds into the film (negative in the pre-show).
ALTER TABLE link_screenings_out ADD COLUMN play_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE link_screenings_out ADD COLUMN play_state TEXT NOT NULL DEFAULT '';
ALTER TABLE link_screenings_out ADD COLUMN play_pos REAL NOT NULL DEFAULT 0;
ALTER TABLE link_screenings_out ADD COLUMN play_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE link_screenings_in ADD COLUMN play_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE link_screenings_in ADD COLUMN play_state TEXT NOT NULL DEFAULT '';
ALTER TABLE link_screenings_in ADD COLUMN play_pos REAL NOT NULL DEFAULT 0;
ALTER TABLE link_screenings_in ADD COLUMN play_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE link_screenings_in ADD COLUMN play_heard INTEGER NOT NULL DEFAULT 0;
ALTER TABLE link_screenings_in ADD COLUMN anyone_pause INTEGER NOT NULL DEFAULT 0;

-- Host side: the host's own household's answers (the one who booked it is
-- going unless they say otherwise); the roster lists them first.
CREATE TABLE link_screening_host_people (
    showtime_id INTEGER NOT NULL REFERENCES showtimes(id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    answer      TEXT    NOT NULL,
    at_ms       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (showtime_id, user_id)
);
