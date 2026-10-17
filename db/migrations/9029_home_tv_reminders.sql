-- Usherr: Live TV reminders. Someone asks to be told when a show starts;
-- a phone message goes out a few minutes before (the "reminder" event).
CREATE TABLE tv_reminders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL,
    channel_id INTEGER NOT NULL,
    start_ms   INTEGER NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    subtitle   TEXT NOT NULL DEFAULT '',
    sent       INTEGER NOT NULL DEFAULT 0,
    UNIQUE (user_id, channel_id, start_ms)
);
CREATE INDEX tv_reminders_due ON tv_reminders(sent, start_ms);

-- Targets that chose their events get reminders too (they can switch them off).
UPDATE notification_targets SET events = events || ',reminder' WHERE events <> '' AND events NOT LIKE '%reminder%';
