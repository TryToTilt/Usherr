package api

// Usherr: Read-to-Me's own text for a chapter. Before a chapter is read (or to
// read it again), its text can be put right: a word the ebook has wrong, a
// footnote or a line that shouldn't be read aloud. The ebook itself is never
// changed; the edit is kept with the book and used wherever Read-to-Me reads
// it. An edit belongs to the chapter text it was made from: when the ebook
// changes, it no longer applies.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

const rtvEditMax = 400_000 // the longest chapter text taken, in bytes

// readingBook is the book as Read-to-Me reads it: the ebook, with the
// chapters' own texts in place.
func (s *Server) readingBook(ctx context.Context, b books.Book) (rtvBook, error) {
	book, err := readEPUBBook(b.EbookPath)
	if err != nil {
		return book, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT chapter, hash, text FROM readtome_edits WHERE book_id = ?`, b.ID)
	if err != nil {
		return book, nil
	}
	defer rows.Close()
	for rows.Next() {
		var ch int
		var hash, text string
		if rows.Scan(&ch, &hash, &text) == nil && ch >= 0 && ch < len(book.Chapters) && hashOf(book.Chapters[ch].Text) == hash {
			book.Chapters[ch].Text, book.Chapters[ch].Edited = text, true
		}
	}
	return book, nil
}

// editTarget is the book and chapter a text route names.
func (s *Server) editTarget(w http.ResponseWriter, r *http.Request, bookID int64, chapter int) (books.Book, rtvBook, bool) {
	b, err := s.BookRepo.Get(bookID)
	if err != nil || b.EbookPath == "" || !strings.EqualFold(b.EbookFormat, "epub") {
		writeError(w, http.StatusNotFound, "That book has no EPUB in the library.")
		return b, rtvBook{}, false
	}
	book, err := readEPUBBook(b.EbookPath)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return b, book, false
	}
	if chapter < 0 || chapter >= len(book.Chapters) {
		writeError(w, http.StatusNotFound, "That chapter isn't in the book.")
		return b, book, false
	}
	return b, book, true
}

// GET /api/home/readtome/text?bookId=&chapter=: a chapter's text as it will be
// read, and whether it is Read-to-Me's own.
func (s *Server) handleReadToMeText(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("bookId"), 10, 64)
	ch, err := strconv.Atoi(r.URL.Query().Get("chapter"))
	if err != nil {
		ch = -1
	}
	b, _, ok := s.editTarget(w, r, id, ch)
	if !ok {
		return
	}
	book, err := s.readingBook(r.Context(), b)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	c := book.Chapters[ch]
	writeJSON(w, http.StatusOK, map[string]any{"title": c.Title, "text": c.Text, "edited": c.Edited})
}

// PUT /api/home/readtome/text {bookId, chapter, text}: the chapter's own text.
func (s *Server) handlePutReadToMeText(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookID  int64  `json:"bookId"`
		Chapter int    `json:"chapter"`
		Text    string `json:"text"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	_, book, ok := s.editTarget(w, r, req.BookID, req.Chapter)
	if !ok {
		return
	}
	text := strings.TrimSpace(strings.ReplaceAll(req.Text, "\r\n", "\n"))
	switch {
	case len(text) > rtvEditMax:
		writeError(w, http.StatusBadRequest, "That text is too long for one chapter.")
		return
	case text == "":
		writeError(w, http.StatusBadRequest, "A chapter needs some text. To leave it out, untick it in What is read.")
		return
	}
	orig := book.Chapters[req.Chapter].Text
	if text == strings.TrimSpace(orig) {
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM readtome_edits WHERE book_id = ? AND chapter = ?`, req.BookID, req.Chapter)
		writeJSON(w, http.StatusOK, map[string]any{"edited": false})
		return
	}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO readtome_edits (book_id, chapter, hash, text, user_id, updated_ms) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(book_id, chapter) DO UPDATE SET hash = excluded.hash, text = excluded.text, user_id = excluded.user_id, updated_ms = excluded.updated_ms`,
		req.BookID, req.Chapter, hashOf(orig), text, requestUserID(r), time.Now().UnixMilli())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"edited": true})
}

// DELETE /api/home/readtome/text?bookId=&chapter=: back to the ebook's text.
func (s *Server) handleDeleteReadToMeText(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("bookId"), 10, 64)
	ch, _ := strconv.Atoi(r.URL.Query().Get("chapter"))
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM readtome_edits WHERE book_id = ? AND chapter = ?`, id, ch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"edited": false})
}
