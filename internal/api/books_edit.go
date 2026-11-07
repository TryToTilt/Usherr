package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

// Correcting a book: its details by hand, or matching it again against the
// search sources. Only the database changes. The files stay exactly where they
// are, under their old names; nothing has ever moved a book's file for a new
// author, and a move wouldn't be worth the risk to a filed library.

const (
	bookTitleMax       = 300
	bookAuthorMax      = 200
	bookSeriesMax      = 200
	bookPositionMax    = 16
	bookDescriptionMax = 20000
	bookYearMax        = 9999
)

type bookEditRequest struct {
	Title          *string `json:"title"`
	Author         *string `json:"author"`
	SeriesName     *string `json:"seriesName"`
	SeriesPosition *string `json:"seriesPosition"`
	Year           *int    `json:"year"`
	Description    *string `json:"description"`
}

func oneLineText(s string) string { return strings.Join(strings.Fields(s), " ") }

// PUT /api/books/{id}: saves the fields that were sent.
func (s *Server) handleEditBook(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	var req bookEditRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	d := books.Details{Title: b.Title, Author: b.Author, AuthorKey: b.AuthorKey, Year: b.Year, Description: b.Description, SeriesName: b.SeriesName, SeriesPosition: b.SeriesPosition}
	if req.Title != nil {
		d.Title = oneLineText(*req.Title)
	}
	if req.Author != nil {
		d.Author = oneLineText(*req.Author)
	}
	if req.SeriesName != nil {
		d.SeriesName = oneLineText(*req.SeriesName)
	}
	if req.SeriesPosition != nil {
		d.SeriesPosition = strings.TrimSpace(*req.SeriesPosition)
	}
	if req.Year != nil {
		d.Year = *req.Year
	}
	if req.Description != nil {
		d.Description = strings.TrimSpace(*req.Description)
	}
	switch {
	case d.Title == "":
		writeError(w, http.StatusBadRequest, "A book needs a title.")
		return
	case len(d.Title) > bookTitleMax || len(d.Author) > bookAuthorMax || len(d.SeriesName) > bookSeriesMax:
		writeError(w, http.StatusBadRequest, "That title, author or series is too long.")
		return
	case len(d.Description) > bookDescriptionMax:
		writeError(w, http.StatusBadRequest, "That description is too long.")
		return
	case d.Year < 0 || d.Year > bookYearMax:
		writeError(w, http.StatusBadRequest, "Enter the year as a number, such as 1937, or leave it empty.")
		return
	case len(d.SeriesPosition) > bookPositionMax:
		writeError(w, http.StatusBadRequest, "Enter the number in the series, such as 2 or 2.5.")
		return
	case d.SeriesPosition != "" && d.SeriesName == "":
		writeError(w, http.StatusBadRequest, "Enter the series name to give the book a number in it.")
		return
	}
	if d.SeriesPosition != "" {
		if _, ok := books.PositionNumber(d.SeriesPosition); !ok {
			writeError(w, http.StatusBadRequest, "Enter the number in the series, such as 2 or 2.5.")
			return
		}
	}
	// The author link and author page go by the key, which belongs to the old name.
	if !strings.EqualFold(d.Author, b.Author) {
		d.AuthorKey = ""
	}
	lock := d.SeriesName != b.SeriesName || d.SeriesPosition != b.SeriesPosition
	if err := s.BookRepo.UpdateDetails(b.ID, d, lock); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, byline := requester(r)
	_ = s.QueueRepo.LogActivity(0, "edited", fmt.Sprintf("The details of %s were changed%s", d.Title, byline))
	s.respondBook(w, r, b.ID)
}

func (s *Server) respondBook(w http.ResponseWriter, r *http.Request, id int64) {
	fresh, err := s.BookRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toBookPayload(fresh, isAdminRequest(r)))
}

// GET /api/books/{id}/file-details: what the book's own file says about itself.
func (s *Server) handleBookFileDetails(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	nothing := map[string]any{"found": false}
	if !hasEbook(b) || ebookExt(b) != "epub" {
		writeJSON(w, http.StatusOK, nothing)
		return
	}
	f, _, full, err := s.openEbook(b)
	if err != nil {
		slog.Info("books: open file for its details", "book", b.ID, "err", err)
		writeJSON(w, http.StatusOK, nothing)
		return
	}
	f.Close()
	d, err := books.ReadFileDetails(full)
	if err != nil {
		slog.Info("books: read file details", "book", b.ID, "err", err)
		writeJSON(w, http.StatusOK, nothing)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": true, "details": d})
}

type bookRematchRequest struct {
	OLKey       string `json:"olKey"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	AuthorKey   string `json:"authorKey"`
	Year        int    `json:"year"`
	CoverID     int    `json:"coverId"`
	HardcoverID int    `json:"hardcoverId"`
	GoogleID    string `json:"googleId"`
}

// POST /api/books/{id}/rematch: makes the book the one picked from the search
// results, with that one's details and cover. A cover given by hand goes too:
// a wrong match's cover is usually why the book is being matched again, and it
// can't be told from one a person chose.
func (s *Server) handleRematchBook(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	var req bookRematchRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.OLKey) == "" || strings.TrimSpace(req.Title) == "" ||
		len(req.Title) > bookTitleMax || len(req.Author) > bookAuthorMax || len(req.OLKey) > 200 {
		writeError(w, http.StatusBadRequest, "Pick a book from the search results.")
		return
	}
	next := books.Book{OLKey: strings.TrimPrefix(strings.TrimSpace(req.OLKey), "/works/"), Title: strings.TrimSpace(req.Title), Author: strings.TrimSpace(req.Author),
		AuthorKey: req.AuthorKey, Year: req.Year, CoverID: req.CoverID}
	gbID := ""
	if id, ok := books.ParseGoogleKey(next.OLKey); ok {
		gbID = id
	}
	if books.ValidGoogleID(strings.TrimSpace(req.GoogleID)) {
		gbID = strings.TrimSpace(req.GoogleID)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	hcID, cover := s.prepareNewBook(ctx, &next, req.HardcoverID)
	if next.Description == "" && gbID != "" {
		if info, err := s.googleWork(ctx, gbID); err == nil {
			next.Description = info.Description
		}
	}
	err := s.BookRepo.Rematch(b.ID, next)
	switch {
	case errors.Is(err, books.ErrExists):
		writeError(w, http.StatusConflict, fmt.Sprintf("%s is already in your library as another book.", next.Title))
		return
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "That book isn't in your library.")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.BookRepo.ClearOwnCover(b.ID); err != nil {
		slog.Warn("books: clear the old cover", "book", b.ID, "err", err)
	}
	fresh, err := s.BookRepo.Get(b.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if hcID > 0 && fresh.CoverID == 0 {
		s.keepHardcoverCover(ctx, fresh, hcID, cover)
	}
	if gbID != "" && fresh.CoverID == 0 {
		if again, err := s.BookRepo.Get(b.ID); err == nil && again.OwnCover == 0 {
			s.keepGoogleCover(ctx, b.ID, gbID)
		}
	}
	_, byline := requester(r)
	_ = s.QueueRepo.LogActivity(0, "edited", fmt.Sprintf("%s was matched again as %s by %s%s", b.Title, next.Title, next.Author, byline))
	s.lookUpAudioDetailsLater(fresh)
	s.respondBook(w, r, b.ID)
}
