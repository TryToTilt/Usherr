-- Usherr: Read-to-Me's full cast. Who says each line of dialogue in a book,
-- where it isn't plain from the text: what the AI worked out ('ai') and what
-- someone set by hand ('you', which wins). A line is its chapter and its place
-- among the chapter's quotes, with a hash of its words so a line that has
-- changed isn't given someone else's voice.
CREATE TABLE readtome_lines (
    book_id   INTEGER NOT NULL,
    chapter   INTEGER NOT NULL,
    quote     INTEGER NOT NULL,
    hash      TEXT NOT NULL DEFAULT '',
    speaker   TEXT NOT NULL,
    source    TEXT NOT NULL, -- ai, you
    PRIMARY KEY (book_id, chapter, quote, source)
);
-- The characters' genders, when the AI knows them (for the "other men" and
-- "other women" voices), and the chapters the AI has gone through.
CREATE TABLE readtome_people (
    book_id INTEGER NOT NULL,
    name    TEXT NOT NULL,
    gender  TEXT NOT NULL DEFAULT '', -- m, f or ''
    PRIMARY KEY (book_id, name)
);
CREATE TABLE readtome_ai_chapters (
    book_id INTEGER NOT NULL,
    chapter INTEGER NOT NULL,
    hash    TEXT NOT NULL,
    PRIMARY KEY (book_id, chapter)
);
