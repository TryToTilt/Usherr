-- The country a show comes from, as TMDB lists it ("US", "GB"; several are
-- joined with commas). It tells "Shameless (US)" and "Shameless (UK)" apart.
-- NULL means not read yet: it is filled in the next time the show is refreshed
-- (or by the background job), so nothing is fetched while the database is
-- upgraded. An empty string means TMDB lists no country.
ALTER TABLE series ADD COLUMN origin_country TEXT;
