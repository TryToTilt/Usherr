package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/kosync"
)

// KOSync: KOReader and Readest keep their place in a book in step through this
// small protocol. Usherr answers it at /kosync (the address on the profile
// page), and at the bare paths too, so an app pointed at Usherr's plain address
// works as well. The answers follow koreader-sync-server, the statuses and
// error codes included; the kosync-conformance spec (CC0) lists why each
// matters. Accounts sign in with their user name and app password; making
// accounts from an app is refused, as the protocol allows.
//
// What travels between the two sides (see kosyncPlaceFor and kosyncFollow):
// a place KOReader reported comes back to KOReader word for word. Usherr's
// reader takes it by percentage, or by page for a PDF or comic. Going the other
// way an EPUB place can only be said down to the start of the chapter: Usherr
// knows a CFI, KOReader wants an XPointer, and those can't be converted without
// KOReader's own view of the page.

const (
	kosyncBodyLimit = 64 << 10
	// kosyncSameBook is how far apart (percent) two places count as the same one.
	kosyncSameBook = 0.5
	// usherrDevice is the name a place from Usherr's reader goes by.
	usherrDevice = "Usherr"
	// kosyncRefreshGap is the least time between looks over the library for a
	// document nobody knows, unless the book list changed.
	kosyncRefreshGap = 10 * time.Minute
	// kosyncRefreshWait is how long a request waits for that look; it goes on
	// in the background, and the next request finds the answer.
	kosyncRefreshWait = 2 * time.Second
)

// KOSync's error codes and their statuses (koreader-sync-server config/errors.lua).
const (
	kosyncInternalError = 2000
	kosyncUnauthorized  = 2001
	kosyncInvalidFields = 2003
	kosyncNoDocument    = 2004
	kosyncNoRegistering = 2005
	kosyncBadDocumentID = 2007
)

// The most a place may say, so a stray app can't fill the database.
const (
	kosyncDeviceFieldMax = 128
	kosyncProgressMax    = 2048
)

// A document id comes back in a path, so it has to be safe to read back.
var kosyncDocument = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,128}$`)

// kosyncRuntime is what the routes keep in memory.
type kosyncRuntime struct {
	mu      sync.Mutex
	running chan struct{} // set while a look over the library is going on
	lastAt  time.Time
	lastVer int64
}

func (s *Server) kosyncRoutes(root *http.ServeMux) {
	h := noStore(http.HandlerFunc(s.handleKOSync))
	root.Handle("/kosync/", http.StripPrefix("/kosync", h))
	for _, p := range []string{"/healthcheck", "/users/auth", "/users/create", "/syncs/progress", "/syncs/progress/"} {
		root.Handle(p, h)
	}
}

func kosyncError(w http.ResponseWriter, status, code int, message string) {
	writeJSON(w, status, map[string]any{"message": message, "code": code})
}

// handleKOSync answers every KOSync address. The Accept header isn't checked:
// the reference server wants KOReader's own, but the spec lets a server serve
// without it.
func (s *Server) handleKOSync(w http.ResponseWriter, r *http.Request) {
	if !s.ebooksEnabled() {
		http.NotFound(w, r)
		return
	}
	switch p := r.URL.Path; {
	case p == "/healthcheck" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]string{"state": "OK"})
	case p == "/users/create" && r.Method == http.MethodPost:
		kosyncError(w, http.StatusPaymentRequired, kosyncNoRegistering, "User registration is disabled. Sign in with your Usherr user name and app password.")
	case p == "/users/auth" && r.Method == http.MethodGet:
		if user, ok := s.kosyncUser(w, r); ok && user != nil {
			s.kosyncLookOverLibraryLater()
			writeJSON(w, http.StatusOK, map[string]string{"authorized": "OK"})
		}
	case p == "/syncs/progress" && r.Method == http.MethodPut:
		if user, ok := s.kosyncUser(w, r); ok {
			s.kosyncPut(w, r, user)
		}
	case strings.HasPrefix(p, "/syncs/progress/") && r.Method == http.MethodGet:
		if user, ok := s.kosyncUser(w, r); ok {
			s.kosyncGet(w, r, user, strings.TrimPrefix(p, "/syncs/progress/"))
		}
	default:
		http.NotFound(w, r)
	}
}

// kosyncUser signs a request in. On failure it has already answered.
func (s *Server) kosyncUser(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	rt := s.ereaderState()
	ipKey := s.ereaderLimitKey(r)
	if !rt.failIP.Allow(ipKey) {
		w.Header().Set("Retry-After", "900")
		kosyncError(w, http.StatusTooManyRequests, kosyncUnauthorized, "Too many wrong tries. Wait a few minutes and try again.")
		return nil, false
	}
	name, key := r.Header.Get("x-auth-user"), r.Header.Get("x-auth-key")
	if name == "" && key == "" {
		name, key, _ = r.BasicAuth() // Readest tries this after a 401
	}
	userKey := strings.ToLower(strings.TrimSpace(name))
	if len(userKey) > 128 {
		userKey = userKey[:128]
	}
	if !rt.failUser.Allow(userKey) {
		w.Header().Set("Retry-After", "900")
		kosyncError(w, http.StatusTooManyRequests, kosyncUnauthorized, "Too many wrong tries for this account. Wait a few minutes and try again.")
		return nil, false
	}
	refuse := func() (*auth.User, bool) {
		rt.failIP.RecordFailure(ipKey)
		rt.failUser.RecordFailure(userKey)
		slog.Warn("kosync: wrong app password", "user", userKey, "ip", s.clientIP(r)) // the name, never the key
		kosyncError(w, http.StatusUnauthorized, kosyncUnauthorized, "Unauthorized")
		return nil, false
	}
	if name == "" || key == "" {
		return refuse()
	}
	user, err := s.Auth.UserByName(name)
	if err != nil {
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			slog.Error("kosync: look up account", "err", err)
			kosyncError(w, http.StatusBadGateway, kosyncInternalError, "Unknown server error.")
			return nil, false
		}
		s.EReader.CheckKOSyncKey(0, key) // the same work as for a real account
		return refuse()
	}
	if !s.EReader.CheckKOSyncKey(user.ID, key) {
		return refuse()
	}
	if ok, why := s.ereaderUserAllowed(user.ID); !ok {
		kosyncError(w, http.StatusUnauthorized, kosyncUnauthorized, why)
		return nil, false
	}
	rt.failIP.RecordSuccess(ipKey)
	return user, true
}

// PUT /syncs/progress: an app reports where it is.
func (s *Server) kosyncPut(w http.ResponseWriter, r *http.Request, user *auth.User) {
	var body struct {
		Document   string          `json:"document"`
		Percentage json.RawMessage `json:"percentage"`
		Progress   json.RawMessage `json:"progress"`
		Device     *string         `json:"device"`
		DeviceID   string          `json:"device_id"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, kosyncBodyLimit))
	if err := dec.Decode(&body); err != nil {
		kosyncError(w, http.StatusForbidden, kosyncInvalidFields, "Invalid request")
		return
	}
	if body.Document == "" || strings.Contains(body.Document, ":") {
		kosyncError(w, http.StatusForbidden, kosyncNoDocument, "Field 'document' not provided.")
		return
	}
	if !kosyncDocument.MatchString(body.Document) {
		kosyncError(w, http.StatusForbidden, kosyncBadDocumentID, "Field 'document' contains characters the read route cannot serve.")
		return
	}
	pct, ok := kosyncNumber(body.Percentage)
	progress, hasProgress := kosyncString(body.Progress)
	if !ok || !hasProgress || body.Device == nil || len(progress) > kosyncProgressMax || len(*body.Device) > kosyncDeviceFieldMax || len(body.DeviceID) > kosyncDeviceFieldMax {
		kosyncError(w, http.StatusForbidden, kosyncInvalidFields, "Invalid request")
		return
	}
	now := time.Now().Unix()
	p := kosync.Position{Document: body.Document, Percentage: pct, Progress: progress, Device: *body.Device, DeviceID: body.DeviceID, Timestamp: now}
	if err := s.kosyncRepo().Save(user.ID, p); err != nil {
		slog.Error("kosync: save", "err", err)
		kosyncError(w, http.StatusBadGateway, kosyncInternalError, "Unknown server error.")
		return
	}
	if b, ok := s.kosyncBook(r.Context(), body.Document); ok {
		s.kosyncFollow(user.ID, b, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"document": body.Document, "timestamp": now})
}

// kosyncFollow moves Usherr's own place in the book to what an app reported.
func (s *Server) kosyncFollow(userID int64, b books.Book, p kosync.Position) {
	percent := math.Max(0, math.Min(100, p.Percentage*100))
	old, had, err := s.BookRepo.GetProgress(userID, b.ID, books.Ebook)
	if err != nil {
		slog.Error("kosync: read place", "err", err)
		return
	}
	// An app that was opened and shut at the first page says 0%. That is no
	// reason to lose a place further on.
	if had && percent < 0.1 && old.Percent > percent {
		return
	}
	if had && math.Abs(old.Percent-percent) < 0.05 {
		return
	}
	next := books.Progress{BookID: b.ID, Format: books.Ebook, Percent: percent, Finished: percent >= 99.5}
	// The reader opens an EPUB by the percentage when there is no CFI, as for a
	// Kobo. A paged book's place can be said exactly.
	if pagedBook(ebookExt(b)) {
		if n, err := strconv.Atoi(p.Progress); err == nil && n > 0 {
			next.Position = "page:" + p.Progress
		}
	}
	if err := s.BookRepo.SetProgress(userID, next); err != nil {
		slog.Error("kosync: save place", "err", err)
	}
}

// GET /syncs/progress/{document}: an app asks where to go. A document nobody
// reported is an empty object with status 200: that is how the apps tell.
func (s *Server) kosyncGet(w http.ResponseWriter, r *http.Request, user *auth.User, doc string) {
	if doc == "" || strings.Contains(doc, ":") {
		kosyncError(w, http.StatusForbidden, kosyncNoDocument, "Field 'document' not provided.")
		return
	}
	p, ok, err := s.kosyncRepo().Get(user.ID, doc)
	if err != nil {
		slog.Error("kosync: read", "err", err)
		kosyncError(w, http.StatusBadGateway, kosyncInternalError, "Unknown server error.")
		return
	}
	if kosyncDocument.MatchString(doc) {
		if b, found := s.kosyncBook(r.Context(), doc); found {
			if newer, changed := s.kosyncPlaceFor(user.ID, b, doc, p, ok); changed {
				p, ok = newer, true
			}
		}
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	out := map[string]any{"document": p.Document, "percentage": p.Percentage, "progress": p.Progress, "device": p.Device, "timestamp": p.Timestamp}
	if p.DeviceID != "" {
		out["device_id"] = p.DeviceID
	}
	writeJSON(w, http.StatusOK, out)
}

// kosyncPlaceFor says what to tell an app when Usherr's reader (or a Kobo) has
// moved since the app last reported. changed is false when the app's own last
// place stands: it is newer or the same, or Usherr's can't be said in a form
// the app can open (a Kindle book, a percentage for a PDF).
func (s *Server) kosyncPlaceFor(userID int64, b books.Book, doc string, have kosync.Position, hasOwn bool) (kosync.Position, bool) {
	up, had, err := s.BookRepo.GetProgress(userID, b.ID, books.Ebook)
	if err != nil || !had {
		return kosync.Position{}, false
	}
	at, err := time.Parse("2006-01-02T15:04:05Z", up.UpdatedAt)
	if err != nil {
		return kosync.Position{}, false
	}
	if hasOwn && (at.Unix() <= have.Timestamp || math.Abs(have.Percentage*100-up.Percent) < kosyncSameBook) {
		return kosync.Position{}, false
	}
	var place string
	switch ext := ebookExt(b); {
	case pagedBook(ext):
		if page, ok := strings.CutPrefix(up.Position, "page:"); ok {
			place = page
		}
	case ext == "epub":
		place = s.kosyncChapterOf(b, up)
	}
	if place == "" {
		return kosync.Position{}, false
	}
	return kosync.Position{Document: doc, Percentage: up.Percent / 100, Progress: place, Device: usherrDevice, DeviceID: "usherr", Timestamp: at.Unix()}, true
}

// kosyncChapterOf is the XPointer of the chapter Usherr's place is in.
func (s *Server) kosyncChapterOf(b books.Book, up books.Progress) string {
	f, _, full, err := s.openEbook(b)
	if err != nil {
		return ""
	}
	f.Close()
	sizes, err := books.EPUBSpineSizes(full)
	if err != nil {
		return ""
	}
	index, ok := kosync.SpineIndexOfCFI(up.Position)
	if !ok || index >= len(sizes) {
		if index, ok = kosync.SpineIndexOfPercent(up.Percent, sizes); !ok {
			return ""
		}
	}
	return kosync.ChapterXPointer(index)
}

// pagedBook reports whether apps give a place in this kind of file as a page.
func pagedBook(ext string) bool { return ext == "pdf" || books.ComicFormat(ext) }

// kosyncBook finds the library book a document id belongs to. A document that
// isn't known starts a look over the library, for a short wait.
func (s *Server) kosyncBook(ctx context.Context, doc string) (books.Book, bool) {
	if b, ok := s.kosyncKnownBook(doc); ok {
		return b, true
	}
	done := s.kosyncLookOverLibraryLater()
	if done == nil {
		return books.Book{}, false
	}
	select {
	case <-done:
	case <-time.After(kosyncRefreshWait):
		return books.Book{}, false
	case <-ctx.Done():
		return books.Book{}, false
	}
	return s.kosyncKnownBook(doc)
}

// kosyncKnownBook is the book on record for a document id, while its file is
// still the one that id was worked out from.
func (s *Server) kosyncKnownBook(doc string) (books.Book, bool) {
	d, ok, err := s.kosyncRepo().BookFor(doc)
	if err != nil {
		slog.Error("kosync: find book", "err", err)
		return books.Book{}, false
	}
	if !ok {
		return books.Book{}, false
	}
	b, err := s.BookRepo.Get(d.BookID)
	if err != nil || !hasEbook(b) || b.EbookPath != d.Path {
		return books.Book{}, false
	}
	info, err := os.Stat(b.EbookPath)
	if err != nil || fileSig(info) != d.Sig {
		return books.Book{}, false
	}
	return b, true
}

// kosyncLookOverLibraryLater works out the document ids of the books that have
// a new file, in the background. It returns what to wait on, or nil when the
// library was looked over a moment ago and hasn't changed since.
func (s *Server) kosyncLookOverLibraryLater() <-chan struct{} {
	rt := &s.kosyncRT
	ver, _ := s.BookRepo.Version()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.running != nil {
		return rt.running
	}
	if !rt.lastAt.IsZero() && ver == rt.lastVer && time.Since(rt.lastAt) < kosyncRefreshGap {
		return nil
	}
	done := make(chan struct{})
	rt.running = done
	s.background(func() {
		s.kosyncLookOverLibrary()
		rt.mu.Lock()
		rt.running, rt.lastAt, rt.lastVer = nil, time.Now(), ver
		rt.mu.Unlock()
		close(done)
	})
	return done
}

func (s *Server) kosyncLookOverLibrary() {
	list, err := s.ebookBooks()
	if err != nil {
		slog.Error("kosync: list books", "err", err)
		return
	}
	repo := s.kosyncRepo()
	known, err := repo.Digests()
	if err != nil {
		slog.Error("kosync: read document ids", "err", err)
		return
	}
	for _, b := range list {
		have, seen := known[b.ID]
		delete(known, b.ID)
		f, info, full, err := s.openEbook(b)
		if err != nil {
			if seen {
				_ = repo.ForgetDigest(b.ID)
			}
			continue
		}
		f.Close()
		if seen && have.Path == b.EbookPath && have.Sig == fileSig(info) {
			continue
		}
		digest, err := kosync.PartialMD5File(full)
		if err != nil {
			slog.Info("kosync: read book", "book", b.ID, "err", err)
			continue
		}
		name := filepath.Base(b.EbookPath)
		if err := repo.SaveDigest(kosync.Digest{BookID: b.ID, Path: b.EbookPath, Sig: fileSig(info), Digest: digest, NameDigest: kosync.MD5Hex(name)}); err != nil {
			slog.Error("kosync: save document ids", "err", err)
			return
		}
	}
	for id := range known { // books whose file went away
		_ = repo.ForgetDigest(id)
	}
}

func (s *Server) kosyncRepo() *kosync.Repo { return kosync.NewRepo(s.db) }

// kosyncNumber reads a number the way the reference server's tonumber does: a
// JSON number, or one in a string.
func kosyncNumber(raw json.RawMessage) (float64, bool) {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		return 0, false
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// kosyncString reads the place: a string, kept as it is, or a page as a number.
func kosyncString(raw json.RawMessage) (string, bool) {
	if raw == nil || string(raw) == "null" {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return fmt.Sprint(n), true
	}
	return "", false
}
