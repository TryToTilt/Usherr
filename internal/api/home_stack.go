package api

// Usherr: the Stack. Each person pulls films and shows out of Discover, the
// Library and title pages onto a stack in the sidebar, to watch next: in
// the order they choose, played from there, opened, moved or taken off.
// Kept on the server, so the phone and the computer share it.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/library"
)

const stackMax = 50 // the "full" message says 50 too

type stackItem struct {
	Kind       string `json:"kind"` // "movie" or "tv"
	TMDBID     int    `json:"tmdbId"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	PosterURL  string `json:"posterUrl,omitempty"`
	LibraryID  int64  `json:"libraryId,omitempty"`  // in the library
	Downloaded bool   `json:"downloaded,omitempty"` // a film with its file, to play at once
}

func stackKey(kind string, tmdbID int) string { return kind + ":" + strconv.Itoa(tmdbID) }

// stackList is a person's stack, top first, with what the library has of each.
func (s *Server) stackList(ctx context.Context, userID int64) ([]stackItem, error) {
	out := []stackItem{}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, tmdb_id, title, year, poster_url FROM user_stack WHERE user_id = ? ORDER BY position, added_ms DESC`, userID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var it stackItem
		if err := rows.Scan(&it.Kind, &it.TMDBID, &it.Title, &it.Year, &it.PosterURL); err != nil {
			return out, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for i := range out {
		it := &out[i]
		if it.Kind == "movie" {
			if m, ok, err := s.MovieRepo.GetByTMDBID(it.TMDBID); err == nil && ok {
				it.LibraryID, it.Downloaded = m.ID, m.Status == library.StatusDownloaded
			}
		} else if sr, ok, err := s.MovieRepo.GetSeriesByTMDBID(it.TMDBID); err == nil && ok {
			it.LibraryID = sr.ID
		}
	}
	return out, nil
}

func (s *Server) writeStack(w http.ResponseWriter, r *http.Request, userID int64) {
	list, err := s.stackList(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Your stack couldn't be read.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func stackUser(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return 0, false
	}
	return u.ID, true
}

// GET /api/home/stack (member): your stack, top first.
func (s *Server) handleGetStack(w http.ResponseWriter, r *http.Request) {
	if uid, ok := stackUser(w, r); ok {
		s.writeStack(w, r, uid)
	}
}

// POST /api/home/stack (member): put a title on top of your stack.
func (s *Server) handleAddStack(w http.ResponseWriter, r *http.Request) {
	uid, ok := stackUser(w, r)
	if !ok {
		return
	}
	var it stackItem
	if err := decodeJSON(r, &it); err != nil || (it.Kind != "movie" && it.Kind != "tv") || it.TMDBID <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't a film or a show.")
		return
	}
	it.Title = trimRunes(strings.TrimSpace(it.Title), 200)
	if it.Year < 0 || it.Year > 3000 {
		it.Year = 0
	}
	if !strings.HasPrefix(it.PosterURL, "https://") && !strings.HasPrefix(it.PosterURL, "/") {
		it.PosterURL = ""
	}
	it.PosterURL = trimRunes(it.PosterURL, 500)
	var count, top int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*), COALESCE(MIN(position), 0) FROM user_stack WHERE user_id = ?`, uid).Scan(&count, &top)
	var have int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM user_stack WHERE user_id = ? AND kind = ? AND tmdb_id = ?`, uid, it.Kind, it.TMDBID).Scan(&have)
	if have == 0 && count >= stackMax {
		writeError(w, http.StatusConflict, "Your stack is full (50 titles). Take one off first.")
		return
	}
	if have == 0 {
		if _, err := s.db.ExecContext(r.Context(), `INSERT INTO user_stack (user_id, kind, tmdb_id, title, year, poster_url, position, added_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			uid, it.Kind, it.TMDBID, it.Title, it.Year, it.PosterURL, top-1, time.Now().UnixMilli()); err != nil {
			writeError(w, http.StatusInternalServerError, "The title couldn't be added to your stack.")
			return
		}
	}
	s.writeStack(w, r, uid)
}

// PUT /api/home/stack/order (member): {"order": ["movie:603", "tv:1396"]}, top
// first. Titles not named keep their places after the named ones.
func (s *Server) handleOrderStack(w http.ResponseWriter, r *http.Request) {
	uid, ok := stackUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Order []string `json:"order"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Order) > stackMax {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	list, err := s.stackList(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Your stack couldn't be read.")
		return
	}
	pos := map[string]int{}
	for i, k := range body.Order {
		if _, dup := pos[k]; !dup {
			pos[k] = i
		}
	}
	next := len(body.Order)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Your stack couldn't be saved.")
		return
	}
	defer func() { _ = tx.Rollback() }()
	for _, it := range list {
		p, named := pos[stackKey(it.Kind, it.TMDBID)]
		if !named {
			p = next
			next++
		}
		if _, err := tx.ExecContext(r.Context(), `UPDATE user_stack SET position = ? WHERE user_id = ? AND kind = ? AND tmdb_id = ?`, p, uid, it.Kind, it.TMDBID); err != nil {
			writeError(w, http.StatusInternalServerError, "Your stack couldn't be saved.")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Your stack couldn't be saved.")
		return
	}
	s.writeStack(w, r, uid)
}

// DELETE /api/home/stack/{kind}/{tmdbId} (member): take a title off your stack.
func (s *Server) handleRemoveStack(w http.ResponseWriter, r *http.Request) {
	uid, ok := stackUser(w, r)
	if !ok {
		return
	}
	id, err := strconv.Atoi(r.PathValue("tmdbId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM user_stack WHERE user_id = ? AND kind = ? AND tmdb_id = ?`, uid, r.PathValue("kind"), id); err != nil {
		writeError(w, http.StatusInternalServerError, "The title couldn't be taken off your stack.")
		return
	}
	s.writeStack(w, r, uid)
}
