package api

// Usherr: where each person got to in a video or a comic (home_positions),
// so it picks up there next time. The player and the comic reader ask on
// opening and tell it as they go. Something finished, or only just begun,
// has no row and starts at the beginning. Live TV and channels never ask:
// they're live. Ebooks and audiobooks keep their place in book_progress.

import (
	"net/http"
	"regexp"
	"time"
)

var positionItemRe = regexp.MustCompile(`^(movie|episode|video|sport|comic|title:movie|title:tv):[0-9a-z:._-]{1,80}$`)

// a video this close to its end (or past this share of it) is finished
const (
	positionDoneShare = 0.95
	positionDoneTail  = 45.0 // seconds
	positionMinStart  = 15.0 // seconds: before this it's not worth resuming
)

func positionFinished(item string, position, duration float64) bool {
	if len(item) > 6 && item[:6] == "comic:" {
		return position <= 1 || (duration > 0 && position >= duration)
	}
	if position < positionMinStart {
		return true
	}
	return duration > 0 && (position >= duration*positionDoneShare || duration-position <= positionDoneTail)
}

// GET /api/home/position?item=...: where this person got to (0 when nowhere).
func (s *Server) handlePositionGet(w http.ResponseWriter, r *http.Request) {
	item := r.URL.Query().Get("item")
	if !positionItemRe.MatchString(item) {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	var pos, dur float64
	_ = s.db.QueryRowContext(r.Context(), `SELECT position, duration FROM home_positions WHERE user_id = ? AND item = ?`, requestUserID(r), item).Scan(&pos, &dur)
	writeJSON(w, http.StatusOK, map[string]any{"position": pos, "duration": dur})
}

// PUT /api/home/position: where this person is now. Finished, or barely
// begun, forgets it, so the next play starts at the beginning.
func (s *Server) handlePositionPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Item     string  `json:"item"`
		Position float64 `json:"position"`
		Duration float64 `json:"duration"`
	}
	if err := decodeJSON(r, &body); err != nil || !positionItemRe.MatchString(body.Item) || body.Position < 0 || body.Duration < 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	user := requestUserID(r)
	var err error
	if positionFinished(body.Item, body.Position, body.Duration) {
		_, err = s.db.ExecContext(r.Context(), `DELETE FROM home_positions WHERE user_id = ? AND item = ?`, user, body.Item)
	} else {
		_, err = s.db.ExecContext(r.Context(), `INSERT INTO home_positions (user_id, item, position, duration, updated_ms) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id, item) DO UPDATE SET position = excluded.position, duration = excluded.duration, updated_ms = excluded.updated_ms`,
			user, body.Item, body.Position, body.Duration, time.Now().UnixMilli())
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't keep the place.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
