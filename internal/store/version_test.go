package store_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/store"
)

// The library version moves on with a change to a movie, show or episode, and
// again when the transaction that made it ends; other writes and reads leave
// it alone, so kept counts are not thrown away by every download's progress.
func TestLibraryVersionFollowsLibraryWrites(t *testing.T) {
	db := openTemp(t)
	if _, err := db.Exec(`INSERT INTO series (tmdb_id, title) VALUES (1, 'Show')`); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		run   func() error
		moves bool
	}{
		{"add a movie", func() error {
			_, err := db.Exec(`INSERT INTO movies (tmdb_id, title) VALUES (1, 'Film')`)
			return err
		}, true},
		{"change an episode", func() error {
			_, err := db.Exec(`INSERT INTO episodes (series_id, season, episode) VALUES (1, 1, 1)`)
			return err
		}, true},
		{"read the library", func() error {
			var n int
			return db.QueryRow(`SELECT COUNT(*) FROM movies`).Scan(&n)
		}, false},
		{"a setting", func() error {
			_, err := db.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('x', 'y')`)
			return err
		}, false},
		{"a column named after a table", func() error {
			_, err := db.Exec(`UPDATE download_queue SET series_id = NULL WHERE id = 0`)
			return err
		}, false},
		{"in a transaction", func() error {
			tx, err := db.Begin()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE series SET monitored = 0 WHERE id = 1`); err != nil {
				return err
			}
			return tx.Commit()
		}, true},
	}
	for _, c := range cases {
		before := store.LibraryVersion()
		if err := c.run(); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if moved := store.LibraryVersion() != before; moved != c.moves {
			t.Errorf("%s: version moved = %v, want %v", c.name, moved, c.moves)
		}
	}

	// The end of a transaction moves it once more, so a count made while the
	// transaction was still open is not kept.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE movies SET title = 'Film 2' WHERE tmdb_id = 1`); err != nil {
		t.Fatal(err)
	}
	during := store.LibraryVersion()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if store.LibraryVersion() == during {
		t.Fatal("the commit did not move the version")
	}
}
