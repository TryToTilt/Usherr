package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/readmarks"
)

// Usherr: bookmarks and highlights in the ebook reader, and bookmarks in the
// audiobook player and comic readers, kept per person (internal/readmarks). A
// book's marks are listed on its page and in the reader or player; a comic's in
// the reader and on its series page. Each one jumps back to where it was made.

var markRepos sync.Map // *Server -> *readmarks.Repo

func (s *Server) marks() *readmarks.Repo {
	if v, ok := markRepos.Load(s); ok {
		return v.(*readmarks.Repo)
	}
	v, _ := markRepos.LoadOrStore(s, readmarks.NewRepo(s.db))
	return v.(*readmarks.Repo)
}

func (s *Server) registerReadingMarkRoutes(member routeGroup) {
	member.HandleFunc("GET /api/books/{id}/marks", s.handleBookMarks)
	member.HandleFunc("POST /api/books/{id}/marks", s.handleAddBookMark)
	member.HandleFunc("GET /api/comics/issues/{id}/marks", s.handleIssueMarks)
	member.HandleFunc("POST /api/comics/issues/{id}/marks", s.handleAddIssueMark)
	member.HandleFunc("GET /api/comics/series/{id}/marks", s.handleSeriesMarks)
	member.HandleFunc("PUT /api/reading-marks/{id}", s.handleUpdateMark)
	member.HandleFunc("DELETE /api/reading-marks/{id}", s.handleDeleteMark)
}

func (s *Server) listMarks(w http.ResponseWriter, r *http.Request, items ...string) {
	list, err := s.marks().List(requestUserID(r), items...)
	if err != nil {
		slog.Error("marks: list", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read your bookmarks.")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) addMark(w http.ResponseWriter, r *http.Request, item string, highlights bool) {
	var m readmarks.Mark
	if err := decodeJSON(r, &m); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	m.Item = item
	if m.Kind == "" {
		m.Kind = readmarks.Bookmark
	}
	if m.Kind == readmarks.Highlight && !highlights {
		writeError(w, http.StatusBadRequest, "Comics and audiobooks have bookmarks, not highlights.")
		return
	}
	if why := m.Problem(); why != "" {
		writeError(w, http.StatusBadRequest, why)
		return
	}
	saved, err := s.marks().Add(requestUserID(r), m)
	if errors.Is(err, readmarks.ErrTooMany) {
		writeError(w, http.StatusConflict, "This book has as many bookmarks as it can hold. Remove some first.")
		return
	}
	if err != nil {
		slog.Error("marks: add", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't keep the bookmark. Try again.")
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

// bookMarkItem is the item a book's marks are kept under. The audiobook's are
// apart from the ebook's (?format=audiobook): a place in sound means nothing to
// the reader, and the other way round.
func bookMarkItem(r *http.Request, id int64) (item string, audio bool) {
	if r.URL.Query().Get("format") == string(books.Audiobook) {
		return "audio:" + strconv.FormatInt(id, 10), true
	}
	return "book:" + strconv.FormatInt(id, 10), false
}

// GET /api/books/{id}/marks
func (s *Server) handleBookMarks(w http.ResponseWriter, r *http.Request) {
	if b, ok := s.bookFromPath(w, r); ok {
		item, _ := bookMarkItem(r, b.ID)
		s.listMarks(w, r, item)
	}
}

// POST /api/books/{id}/marks
func (s *Server) handleAddBookMark(w http.ResponseWriter, r *http.Request) {
	if b, ok := s.bookFromPath(w, r); ok {
		item, audio := bookMarkItem(r, b.ID)
		s.addMark(w, r, item, !audio)
	}
}

func (s *Server) comicsOnOrFail(w http.ResponseWriter) bool {
	if !s.comicsEnabled() {
		writeError(w, http.StatusNotFound, "Comics are switched off. Switch them on in Comics settings.")
		return false
	}
	return true
}

// GET /api/comics/issues/{id}/marks
func (s *Server) handleIssueMarks(w http.ResponseWriter, r *http.Request) {
	if !s.comicsOnOrFail(w) {
		return
	}
	if is, _, ok := s.comicIssueFromPath(w, r); ok {
		s.listMarks(w, r, "comic:"+strconv.FormatInt(is.ID, 10))
	}
}

// POST /api/comics/issues/{id}/marks
func (s *Server) handleAddIssueMark(w http.ResponseWriter, r *http.Request) {
	if !s.comicsOnOrFail(w) {
		return
	}
	if is, _, ok := s.comicIssueFromPath(w, r); ok {
		s.addMark(w, r, "comic:"+strconv.FormatInt(is.ID, 10), false)
	}
}

// GET /api/comics/series/{id}/marks: the bookmarks in every issue of a series.
func (s *Server) handleSeriesMarks(w http.ResponseWriter, r *http.Request) {
	if !s.comicsOnOrFail(w) {
		return
	}
	sr, ok := s.comicSeriesFromPath(w, r)
	if !ok {
		return
	}
	issues, err := s.comicRepo().Issues(sr.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The issues couldn't be read.")
		return
	}
	items := make([]string, 0, len(issues))
	for _, is := range issues {
		items = append(items, "comic:"+strconv.FormatInt(is.ID, 10))
	}
	s.listMarks(w, r, items...)
}

func markIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't a bookmark.")
		return 0, false
	}
	return id, true
}

// PUT /api/reading-marks/{id}: a new note, colour or label.
func (s *Server) handleUpdateMark(w http.ResponseWriter, r *http.Request) {
	id, ok := markIDFromPath(w, r)
	if !ok {
		return
	}
	user := requestUserID(r)
	old, err := s.marks().Get(user, id)
	if errors.Is(err, readmarks.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That bookmark isn't there any more.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't read the bookmark.")
		return
	}
	body := struct {
		Note  *string `json:"note"`
		Color *string `json:"color"`
		Label *string `json:"label"`
	}{}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	note, color, label := old.Note, old.Color, old.Label
	if body.Note != nil {
		note = *body.Note
	}
	if body.Color != nil {
		color = *body.Color
	}
	if body.Label != nil {
		label = *body.Label
	}
	if why := readmarks.ValidUpdate(&note, &color, &label); why != "" {
		writeError(w, http.StatusBadRequest, why)
		return
	}
	m, err := s.marks().Update(user, id, note, color, label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't save the bookmark. Try again.")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// DELETE /api/reading-marks/{id}
func (s *Server) handleDeleteMark(w http.ResponseWriter, r *http.Request) {
	id, ok := markIDFromPath(w, r)
	if !ok {
		return
	}
	err := s.marks().Delete(requestUserID(r), id)
	if errors.Is(err, readmarks.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That bookmark isn't there any more.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't remove the bookmark. Try again.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
