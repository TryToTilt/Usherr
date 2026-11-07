-- Usherr: when a followed series got a new volume, for the calendar.
CREATE TABLE manga_new_volumes (
    volume_id INTEGER PRIMARY KEY REFERENCES manga_volumes(id) ON DELETE CASCADE,
    found_on  TEXT NOT NULL
);
CREATE INDEX manga_new_volumes_day ON manga_new_volumes (found_on);
