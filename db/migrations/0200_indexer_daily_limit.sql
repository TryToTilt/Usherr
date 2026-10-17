-- Daily request limits. Free indexers allow only a number of requests a day
-- (for example 25) and answer "Request limit reached" after that.
--   requests_per_day  the limit a person set for the indexer; 0 = none set
--   learned_limit     the limit the indexer itself reported; 0 = not known
--   quota_back_at     when the indexer said its requests are back (RFC 3339,
--                     empty = it has not said)
-- indexer_stats.requests counts every request Mediarium made to the indexer
-- that day (searches, feed reads, connection checks and release downloads).
ALTER TABLE indexers ADD COLUMN requests_per_day INTEGER NOT NULL DEFAULT 0;
ALTER TABLE indexers ADD COLUMN learned_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE indexers ADD COLUMN quota_back_at TEXT NOT NULL DEFAULT '';
ALTER TABLE indexer_stats ADD COLUMN requests INTEGER NOT NULL DEFAULT 0;
