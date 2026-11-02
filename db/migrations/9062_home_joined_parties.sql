-- Usherr: Community. A watch party joined from another Usherr with its
-- showtime code (Showtime, Join a watch party). The guest's own Usherr gives
-- them a ticket stub at the door and stamps it at the credits, with no link to
-- the host's bot (home_community_guest.go). Its stubs are community_stubs rows
-- with backend 'here' and the guest's account id.
CREATE TABLE community_joined (
    showtime_id INTEGER PRIMARY KEY REFERENCES showtimes(id) ON DELETE CASCADE,
    code        TEXT    NOT NULL,
    joined_ms   INTEGER NOT NULL
);
