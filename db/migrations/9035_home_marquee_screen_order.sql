-- Usherr: the Marquee's screens in an order of your own (Marquee > Screens,
-- the up and down arrows). Screens already there keep the order they came in.
ALTER TABLE marquee_screens ADD COLUMN position INTEGER NOT NULL DEFAULT 0;

UPDATE marquee_screens SET position = (
    SELECT COUNT(*) FROM marquee_screens AS m
    WHERE m.created_ms < marquee_screens.created_ms
       OR (m.created_ms = marquee_screens.created_ms AND m.id < marquee_screens.id)
);
