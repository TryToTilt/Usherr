-- Home fork: AI answers each person has had today, for the daily limit.
CREATE TABLE home_ai_usage (
    user_id INTEGER NOT NULL,
    day     TEXT NOT NULL,
    n       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
);
