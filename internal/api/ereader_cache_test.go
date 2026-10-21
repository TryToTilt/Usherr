package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

// get is a conditional request an OPDS app may make.
func erOPDSGetWith(t *testing.T, url, user, pass, etag string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.SetBasicAuth(user, pass)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := erClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp, erBody(t, resp)
}

func TestOPDSCacheServesFromMemoryAndFollowsTheBooks(t *testing.T) {
	f := newEReaderFixture(t)
	client := erClient()
	access := postJSON[ereaderAccess](t, f.admin, f.base+"/api/auth/ereader/opds", nil, http.StatusOK)
	pw := access.Password

	pages := []string{"/opds/", "/opds/new", "/opds/books", "/opds/authors", "/opds/series", "/opds/search?q=dune", "/opds/v2/books"}
	first := map[string]string{}
	for _, p := range pages {
		resp, data := erOPDSGet(t, client, f.base+p, "ryan", pw)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", p, resp.StatusCode)
		}
		first[p] = string(data)
	}
	lists, feeds := f.server.TestEReaderBuilds()
	if feeds != int64(len(pages)) {
		t.Fatalf("each page should be built once: %d builds for %d pages", feeds, len(pages))
	}
	if lists != 1 {
		t.Fatalf("the book list should be read once for all pages, read %d times", lists)
	}

	// Asking again builds nothing and reads nothing, and gives the same bytes.
	for round := 0; round < 3; round++ {
		for _, p := range pages {
			_, data := erOPDSGet(t, client, f.base+p, "ryan", pw)
			if string(data) != first[p] {
				t.Fatalf("%s changed without a change in the books", p)
			}
		}
	}
	if l2, f2 := f.server.TestEReaderBuilds(); l2 != lists || f2 != feeds {
		t.Fatalf("second round hit the database: lists %d->%d, feeds %d->%d", lists, l2, feeds, f2)
	}

	// Reading progress is not a change to the catalogue.
	if err := f.server.BookRepo.SetProgress(1, books.Progress{BookID: f.hobbit.ID, Format: books.Ebook, Position: "x", Percent: 12}); err != nil {
		t.Fatal(err)
	}
	erOPDSGet(t, client, f.base+"/opds/new", "ryan", pw)
	if _, f2 := f.server.TestEReaderBuilds(); f2 != feeds {
		t.Fatalf("saving a place rebuilt the feed (%d -> %d)", feeds, f2)
	}

	// An ETag from the cached page answers 304 next time.
	resp, _ := erOPDSGetWith(t, f.base+"/opds/new", "ryan", pw, "")
	tag := resp.Header.Get("ETag")
	if tag == "" {
		t.Fatal("no ETag")
	}
	resp, data := erOPDSGetWith(t, f.base+"/opds/new", "ryan", pw, tag)
	if resp.StatusCode != http.StatusNotModified || len(data) != 0 {
		t.Fatalf("conditional request: %d, %d bytes", resp.StatusCode, len(data))
	}
	// The sign-in is still needed for a cached page.
	if resp, _ := erOPDSGet(t, client, f.base+"/opds/new", "ryan", "wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cached page without the right password: %d", resp.StatusCode)
	}

	changes := []struct {
		name   string
		change func(t *testing.T)
		check  func(t *testing.T, newFeed string)
	}{
		{"a book is added", func(t *testing.T) {
			f.addEbook(t, "OL3W", "Emma", "Jane Austen", 1815, false, nil)
		}, func(t *testing.T, s string) {
			if !strings.Contains(s, "Emma") {
				t.Fatal("new book missing from the feed")
			}
		}},
		{"a book is renamed", func(t *testing.T) {
			if _, err := f.server.TestDB().Exec(`UPDATE books SET title = 'Dune Messiah' WHERE id = ?`, f.dune.ID); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, s string) {
			if !strings.Contains(s, "Dune Messiah") {
				t.Fatal("new title missing from the feed")
			}
		}},
		{"a book loses its file", func(t *testing.T) {
			if err := f.server.BookRepo.ClearFile(f.dune.ID, books.Ebook); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, s string) {
			if strings.Contains(s, "Dune Messiah") {
				t.Fatal("book without a file still listed")
			}
		}},
		{"a book is deleted", func(t *testing.T) {
			if err := f.server.BookRepo.Delete(f.hobbit.ID); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, s string) {
			if strings.Contains(s, "The Hobbit") {
				t.Fatal("deleted book still listed")
			}
		}},
	}
	for _, c := range changes {
		t.Run(c.name, func(t *testing.T) {
			_, before := f.server.TestEReaderBuilds()
			c.change(t)
			resp, data := erOPDSGet(t, client, f.base+"/opds/new", "ryan", pw)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%d", resp.StatusCode)
			}
			if _, after := f.server.TestEReaderBuilds(); after != before+1 {
				t.Fatalf("the change did not cause one rebuild: %d -> %d", before, after)
			}
			c.check(t, string(data))
			// And the old tag no longer says "not modified".
			if resp, _ := erOPDSGetWith(t, f.base+"/opds/new", "ryan", pw, tag); resp.StatusCode != http.StatusOK {
				t.Fatalf("old ETag still matches: %d", resp.StatusCode)
			}
		})
	}
}

// A big library: the page asked for again is not built again. The times are
// only logged (they depend on the machine); the count of builds is the check.
func TestOPDSCacheOnABigLibrary(t *testing.T) {
	f := newEReaderFixture(t)
	access := postJSON[ereaderAccess](t, f.admin, f.base+"/api/auth/ereader/opds", nil, http.StatusOK)
	db := f.server.TestDB()
	if _, err := db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 5000)
		INSERT INTO books (ol_key, title, author, description, ebook_status, ebook_format, ebook_path, series_name, series_position)
		SELECT 'OLX' || i, 'Book ' || i, 'Author ' || (i % 400), printf('%.800c', 'x'), 'downloaded', 'epub', '/ebooks/' || i || '.epub',
			'Series ' || (i % 300), i % 12 FROM n`); err != nil {
		t.Fatal(err)
	}
	get := func(p string) (time.Duration, int) {
		start := time.Now()
		resp, data := erOPDSGet(t, erClient(), f.base+p, "ryan", access.Password)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", p, resp.StatusCode)
		}
		return time.Since(start), len(data)
	}
	cold, size := get("/opds/books?page=3")
	_, feeds := f.server.TestEReaderBuilds()
	var hot time.Duration
	for i := 0; i < 20; i++ {
		d, _ := get("/opds/books?page=3")
		hot += d
	}
	if _, after := f.server.TestEReaderBuilds(); after != feeds {
		t.Fatalf("the page was built again: %d -> %d", feeds, after)
	}
	t.Logf("5000 books, one page of %d bytes: first request %v, repeated %v each", size, cold, hot/20)
}
