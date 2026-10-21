package books_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/store"
)

// The change marker moves for what the e-reader feeds show, and only for that.
func TestVersionMovesWithTheBookList(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := books.NewRepo(db)

	version := func() int64 {
		t.Helper()
		v, err := repo.Version()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	var id int64
	steps := []struct {
		name  string
		do    func() error
		moves bool
	}{
		{"add a book", func() error {
			b, err := repo.Add(books.Book{OLKey: "OL1W", Title: "Dune", Author: "Frank Herbert"})
			id = b.ID
			return err
		}, true},
		{"mark it downloaded", func() error {
			return repo.SetState(id, books.Ebook, books.StatusDownloaded, "epub", "/books/dune.epub")
		}, true},
		{"want the audiobook too", func() error { return repo.SetWant(id, books.Audiobook, true) }, false},
		{"audiobook state", func() error { return repo.SetState(id, books.Audiobook, books.StatusDownloading, "", "") }, false},
		{"change the title", func() error {
			_, err := db.Exec(`UPDATE books SET title = 'Dune Messiah' WHERE id = ?`, id)
			return err
		}, true},
		{"forget the file", func() error { return repo.ClearFile(id, books.Ebook) }, true},
		{"delete the book", func() error { return repo.Delete(id) }, true},
	}
	for _, s := range steps {
		before := version()
		if err := s.do(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		after := version()
		if s.moves && after <= before {
			t.Errorf("%s: marker stayed at %d", s.name, before)
		}
		if !s.moves && after != before {
			t.Errorf("%s: marker moved %d -> %d, but the feeds do not show it", s.name, before, after)
		}
	}
}
