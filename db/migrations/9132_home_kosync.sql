-- Usherr: KOSync, reading progress from KOReader and Readest (internal/kosync).

-- The last place each account's apps reported for a document, kept word for
-- word: an app can only use a place in its own format. A document need not be
-- a book in the library.
CREATE TABLE kosync_progress (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    document   TEXT    NOT NULL,
    percentage REAL    NOT NULL,
    progress   TEXT    NOT NULL,
    device     TEXT    NOT NULL,
    device_id  TEXT    NOT NULL DEFAULT '',
    timestamp  INTEGER NOT NULL,
    PRIMARY KEY (user_id, document)
);
CREATE INDEX kosync_progress_recent ON kosync_progress (user_id, timestamp);

-- Which document ids a book's file goes by: KOReader's partial MD5 of the file
-- and the MD5 of its name (KOReader's other setting). Worked out again when
-- the file changes (sig: its size and date).
CREATE TABLE book_kosync_digests (
    book_id     INTEGER PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    sig         TEXT NOT NULL,
    digest      TEXT NOT NULL,
    name_digest TEXT NOT NULL
);
CREATE INDEX book_kosync_digests_digest ON book_kosync_digests (digest);
CREATE INDEX book_kosync_digests_name ON book_kosync_digests (name_digest);
