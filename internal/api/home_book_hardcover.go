package api

// Home fork: books on disk that Open Library doesn't know are looked up on
// Hardcover (books/home_hardcover_match.go). Also tries the file's author
// and title the other way round on Open Library first.

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/rdborg/mediarium/internal/books"
)

// hcCovers holds the cover of each Hardcover-only book (book id -> URL).
var hcCovers sync.Map

func (s *Server) loadHardcoverCovers() {
	if s.db == nil { // route tests build the routes without a database
		return
	}
	rows, err := s.db.Query(`SELECT book_id, url FROM home_book_covers`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var u string
		if rows.Scan(&id, &u) == nil {
			hcCovers.Store(id, u)
		}
	}
}

func (s *Server) setHardcoverCover(bookID int64, url string) {
	if url == "" || !strings.HasPrefix(url, "https://assets.hardcover.app/") {
		return
	}
	if _, err := s.db.Exec(`INSERT INTO home_book_covers (book_id, url) VALUES (?, ?) ON CONFLICT (book_id) DO UPDATE SET url = excluded.url`, bookID, url); err == nil {
		hcCovers.Store(bookID, url)
	}
}

// hardcoverCover is the cover of a Hardcover-only book, or "".
func hardcoverCover(b books.Book) string {
	if !books.IsHardcoverKey(b.OLKey) {
		return ""
	}
	if v, ok := hcCovers.Load(b.ID); ok {
		return v.(string)
	}
	return ""
}

// matchLocalBookWider is tried when Open Library found nothing for a book
// on disk: Open Library again with the author and title swapped (some
// files' details have them the wrong way round), then Hardcover.
func (s *Server) matchLocalBookWider(ctx context.Context, lb books.LocalBook) (*books.Found, *books.HardcoverMatch) {
	if books.LooksSwapped(lb.Author, lb.Title) {
		sw := lb
		sw.Author, sw.Title = lb.Title, lb.Author
		if f, err := s.OpenLibrary.MatchLocal(ctx, sw); err == nil && f != nil {
			return f, nil
		}
	}
	hc := s.hardcover()
	if hc == nil {
		return nil, nil
	}
	m, err := hc.MatchLocal(ctx, lb)
	if err != nil || m == nil {
		return nil, nil
	}
	return nil, m
}

// GET /api/home/hardcover-match?title=&author=: what the import would find
// on Hardcover for a book on disk (for checking a match by hand).
func (s *Server) handleHardcoverMatch(w http.ResponseWriter, r *http.Request) {
	hc := s.hardcover()
	if hc == nil {
		writeError(w, http.StatusPreconditionFailed, "Add your Hardcover key first.")
		return
	}
	lb := books.LocalBook{Title: r.URL.Query().Get("title"), Author: r.URL.Query().Get("author")}
	m, err := hc.MatchLocal(r.Context(), lb)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"match": m})
}
