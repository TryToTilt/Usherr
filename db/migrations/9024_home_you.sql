-- Home fork: the You page's own settings, one row per person.
CREATE TABLE home_you (
    user_id     INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    share_taste INTEGER NOT NULL DEFAULT 0 -- the household may see what you lean towards, and what you have in common
);
