package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/opds"
)

// A cache for the e-reader catalogue. Building a page of the catalogue means
// loading every book, filtering and sorting them, so a reader app that pages
// through a big library, or a Kobo that syncs, made the database do that work
// again for each request. The built pages and the list of ebooks are kept in
// memory instead, and thrown away when the books table's change marker
// (books.Repo.Version, bumped by database triggers) moves. Reading progress is
// not part of the marker, so a reader saving its place leaves the cache alone.
//
// The OPDS catalogue is the same for every account that may sign in (every
// account sees the whole library). The cache key still starts with the account
// so a page built for one account is never handed to another, should that ever
// change.

const (
	opdsCacheEntries = 256
	opdsCacheBytes   = 8 << 20
	// A search or author name longer than this is built every time, not kept.
	opdsCacheMaxQuery = 512
)

// ebookBooks lists the books that have an ebook, oldest first by number. The
// list is shared between requests and must be treated as read-only.
func (s *Server) ebookBooks() ([]books.Book, error) {
	rt := s.ereaderState()
	ver, verr := s.BookRepo.Version()
	rt.listMu.Lock()
	defer rt.listMu.Unlock()
	if verr == nil && rt.listLoaded && rt.listVer == ver {
		return rt.listBooks, nil
	}
	all, err := s.BookRepo.List()
	if err != nil {
		return nil, err
	}
	rt.listBuilds.Add(1)
	out := make([]books.Book, 0, len(all))
	for _, b := range all {
		if hasEbook(b) {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if verr == nil {
		rt.listVer, rt.listBooks, rt.listLoaded = ver, out, true
	}
	return out, nil
}

// feedRecorder catches what a feed handler writes.
type feedRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (f *feedRecorder) Header() http.Header { return f.header }
func (f *feedRecorder) WriteHeader(code int) {
	if f.status == 0 {
		f.status = code
	}
}
func (f *feedRecorder) Write(p []byte) (int, error) {
	f.WriteHeader(http.StatusOK)
	return f.body.Write(p)
}

// opdsCacheKey names a page: who asked, which feed, and the parts of the
// address that change what it holds.
func opdsCacheKey(userID int64, r *http.Request) (string, bool) {
	if len(r.URL.RawQuery) > opdsCacheMaxQuery {
		return "", false
	}
	q := r.URL.Query()
	keep := url.Values{}
	for _, k := range []string{"page", "name", "q"} {
		if v, ok := q[k]; ok {
			keep[k] = v[:1]
		}
	}
	return strconv.FormatInt(userID, 10) + "|" + r.URL.Path + "?" + keep.Encode(), true
}

// serveFeed answers with the page build makes, from the cache when the book
// list has not changed since it was built.
func (s *Server) serveFeed(w http.ResponseWriter, r *http.Request, userID int64, build func(http.ResponseWriter)) {
	rt := s.ereaderState()
	ver, verr := s.BookRepo.Version()
	key, keyOK := opdsCacheKey(userID, r)
	usable := verr == nil && keyOK
	if usable {
		if c, ok := rt.feeds.Lookup(key, ver); ok {
			sendCachedFeed(w, r, c)
			return
		}
	}
	rec := &feedRecorder{header: http.Header{}}
	build(rec)
	rt.feedBuilds.Add(1)
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	if rec.status != http.StatusOK {
		for k, v := range rec.header {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
		return
	}
	sum := sha256.Sum256(rec.body.Bytes())
	c := opds.Cached{Body: rec.body.Bytes(), ContentType: rec.header.Get("Content-Type"), ETag: `"` + hex.EncodeToString(sum[:12]) + `"`}
	if usable {
		rt.feeds.Put(key, ver, c)
	}
	sendCachedFeed(w, r, c)
}

// sendCachedFeed writes a built page, or 304 when the app already has it.
func sendCachedFeed(w http.ResponseWriter, r *http.Request, c opds.Cached) {
	h := w.Header()
	h.Set("Content-Type", c.ContentType)
	h.Set("ETag", c.ETag)
	if etagMatches(r.Header.Get("If-None-Match"), c.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(c.Body)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(c.Body)
	}
}
