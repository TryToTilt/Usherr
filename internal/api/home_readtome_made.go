package api

// Usherr: the audiobooks Read-to-Me has made, for the Read-to-Me tab and the
// dialog on an ebook's page: a list of them, and taking one away. Only a file
// Read-to-Me made itself (a finished job names it) can be deleted here, never
// an audiobook that came from anywhere else, and never the ebook.

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/books"
)

// madeAudiobook is the audiobook Read-to-Me made of a book, if its file is
// still there.
func (s *Server) madeAudiobook(ctx context.Context, bookID int64) (rtvJob, bool) {
	j, err := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM readtome_jobs WHERE book_id = ? AND state = ? AND output_path <> '' ORDER BY id DESC LIMIT 1`, bookID, jobDone))
	if err != nil || !rtvExists(j.OutputPath) {
		return j, false
	}
	return j, true
}

// mayChangeMade: an administrator, or the person who made it.
func (s *Server) mayChangeMade(r *http.Request, j rtvJob) bool {
	return isAdminRequest(r) || (j.UserID != 0 && j.UserID == requestUserID(r))
}

type madeBook struct {
	rtvJob
	SizeBytes int64 `json:"sizeBytes"`
	CanChange bool  `json:"canChange"` // may remake or delete it
	// What it was made of is kept (home_readtome_redo.go): its lines can be read again.
	Kept      bool  `json:"kept"`
	KeptBytes int64 `json:"keptBytes,omitempty"`
	Flagged   int   `json:"flagged,omitempty"` // lines that may be read badly
}

func (s *Server) madeView(r *http.Request, j rtvJob) madeBook {
	m := madeBook{rtvJob: s.jobView(j), CanChange: s.mayChangeMade(r, j)}
	m.OutputPath = "" // the page has no use for where it is on disk
	m.Kept, m.KeptBytes, m.Flagged = s.makingsView(j.BookID)
	if fi, err := os.Stat(j.OutputPath); err == nil {
		m.SizeBytes = fi.Size()
	}
	return m
}

// GET /api/home/readtome/made: every audiobook Read-to-Me made whose file is
// still there, the newest first.
func (s *Server) handleReadToMeMade(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+jobColumns+` FROM readtome_jobs WHERE state = ? AND output_path <> '' ORDER BY done_ms DESC, id DESC`, jobDone)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var jobs []rtvJob
	for rows.Next() {
		if j, err := scanJob(rows); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	out := []madeBook{}
	seen := map[int64]bool{}
	for _, j := range jobs {
		if seen[j.BookID] || !rtvExists(j.OutputPath) {
			continue
		}
		seen[j.BookID] = true
		out = append(out, s.madeView(r, j))
	}
	writeJSON(w, http.StatusOK, map[string]any{"made": out})
}

// audioExts are the files that make a folder an audiobook.
var audioExts = map[string]bool{".m4b": true, ".m4a": true, ".mp3": true, ".flac": true, ".ogg": true, ".opus": true, ".aac": true, ".wma": true}

// hasOtherAudio says whether a folder still holds an audiobook's files.
func hasOtherAudio(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && audioExts[strings.ToLower(filepath.Ext(e.Name()))] {
			return true
		}
	}
	return false
}

// DELETE /api/home/readtome/made/{id}: deletes the audiobook a finished job
// made (the M4B, and the library's note of it when nothing else is left in
// its folder). The ebook stays as it is.
func (s *Server) handleReadToMeDeleteMade(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a made audiobook.")
		return
	}
	j, err := s.getJob(r.Context(), id)
	if err != nil || j.State != jobDone || j.OutputPath == "" || !strings.EqualFold(filepath.Ext(j.OutputPath), ".m4b") {
		writeError(w, http.StatusNotFound, "That audiobook isn't one Read-to-Me made.")
		return
	}
	if !s.mayChangeMade(r, j) {
		writeError(w, http.StatusForbidden, "Only an administrator or the person who made it can delete this audiobook.")
		return
	}
	var active int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM readtome_jobs WHERE book_id = ? AND state IN (?, ?, ?)`, j.BookID, jobQueued, jobRunning, jobWaiting).Scan(&active)
	if active > 0 {
		writeError(w, http.StatusConflict, "This book is being made again. Stop that first.")
		return
	}
	if err := os.Remove(j.OutputPath); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, "The audiobook couldn't be deleted: "+err.Error())
		return
	}
	dir := filepath.Dir(j.OutputPath)
	if b, err := s.BookRepo.Get(j.BookID); err == nil && b.AudioStatus == books.StatusDownloaded && filepath.Clean(b.AudioPath) == filepath.Clean(dir) && !hasOtherAudio(dir) {
		_ = s.BookRepo.ClearFile(b.ID, books.Audiobook)
		_ = s.BookRepo.SetWant(b.ID, books.Audiobook, false)
		_ = os.Remove(dir) // only when it is empty
	}
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM readtome_jobs WHERE book_id = ? AND state = ?`, j.BookID, jobDone)
	s.forgetMakings(j.BookID)
	writeJSON(w, http.StatusOK, map[string]any{})
}
