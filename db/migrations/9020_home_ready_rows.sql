-- Home fork: the ready-made Discover rows (top rated, box office, hidden gems,
-- world cinema) each person has hidden.
CREATE TABLE home_hidden_rows (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    row_key TEXT NOT NULL, -- "movie:top", "tv:gems"
    PRIMARY KEY (user_id, row_key)
);
