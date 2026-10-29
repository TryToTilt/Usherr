-- Usherr: how releases did on Usherr's streaming engine, per release group
-- ("g:ntb") and per indexer ("i:nzbgeek"): opened, or failed to open, broke
-- part-way or were passed over with "Try another release". The engine moves
-- groups and indexers that keep failing to the back of its list.
CREATE TABLE stream_reliability (
    key        TEXT PRIMARY KEY,
    ok         INTEGER NOT NULL DEFAULT 0,
    bad        INTEGER NOT NULL DEFAULT 0,
    updated_ms INTEGER NOT NULL DEFAULT 0
);
