-- Usherr: Community, reviews. One review per person per film or show: stars
-- (1 to 10, half stars out of five; 0 for none) and words. Posted from Usherr
-- or with /review in Discord, or typed as "#review" in a watch party's thread
-- or in a film's post in the reviews forum. Each one has a card in the reviews
-- channel (in a forum, a reply in the film's post), except one typed right
-- there, which is its own card. Usherr keeps its own copy: the film page shows
-- them, and they can be posted again after a move to another server.
CREATE TABLE community_reviews (
    backend        TEXT    NOT NULL DEFAULT 'discord',
    kind           TEXT    NOT NULL,             -- movie or tv
    tmdb_id        INTEGER NOT NULL,
    external_id    TEXT    NOT NULL,             -- the person's Discord id
    user_id        INTEGER REFERENCES users(id) ON DELETE SET NULL, -- their Usherr account, when linked
    name           TEXT    NOT NULL DEFAULT '',
    avatar         TEXT    NOT NULL DEFAULT '',
    stars          INTEGER NOT NULL DEFAULT 0,
    text           TEXT    NOT NULL DEFAULT '',
    title          TEXT    NOT NULL DEFAULT '',
    year           INTEGER NOT NULL DEFAULT 0,
    poster         TEXT    NOT NULL DEFAULT '',
    source         TEXT    NOT NULL,             -- usherr, command or typed
    server         TEXT    NOT NULL DEFAULT '',
    channel        TEXT    NOT NULL DEFAULT '',  -- where its card (or the typed message) is
    message_id     TEXT    NOT NULL DEFAULT '',
    origin_channel TEXT    NOT NULL DEFAULT '',  -- typed: where it was typed
    origin_message TEXT    NOT NULL DEFAULT '',
    origin_label   TEXT    NOT NULL DEFAULT '',  -- typed in a party: "the Heat watch party"
    updated_ms     INTEGER NOT NULL,
    PRIMARY KEY (backend, kind, tmdb_id, external_id)
);
CREATE INDEX community_reviews_origin ON community_reviews (origin_message);

-- The reviews forum's post for each film or show (when reviews go to a forum).
CREATE TABLE community_review_posts (
    backend   TEXT    NOT NULL DEFAULT 'discord',
    forum     TEXT    NOT NULL,
    kind      TEXT    NOT NULL,
    tmdb_id   INTEGER NOT NULL,
    thread_id TEXT    NOT NULL,
    PRIMARY KEY (backend, forum, kind, tmdb_id)
);
CREATE INDEX community_review_posts_thread ON community_review_posts (thread_id);
